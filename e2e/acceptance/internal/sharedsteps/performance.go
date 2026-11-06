package sharedsteps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const stressUserName = "stress"

const (
	// stressDrainTimeout exceeds the completion budget so a slow load fails on the
	// budget with its statistics instead of on the queue wait.
	stressDrainTimeout = 30 * time.Minute
	// stressReservationDuration bounds how long the reserved cores stay held if
	// cleanup never runs; it exceeds the whole scenario.
	stressReservationDuration = 50 * time.Minute
	// stressIdleQueueTimeout outlasts a periodic GPU ActiveCheck, which holds every
	// GPU worker for up to about 13 minutes at 00:00 and 12:00 UTC.
	stressIdleQueueTimeout = 20 * time.Minute
)

// stressSubmitResult is what the submission script prints, measured inside the jail.
type stressSubmitResult struct {
	// StartEpoch and EndEpoch bracket the submission loop in fractional seconds.
	StartEpoch float64 `json:"start_epoch"`
	EndEpoch   float64 `json:"end_epoch"`
	// Accepted is the number of job IDs sbatch printed.
	Accepted int `json:"accepted"`
	// ExitStatus is the status of the submission loop; xargs returns 123 when any sbatch failed.
	ExitStatus int `json:"exit_status"`
}

func (r stressSubmitResult) Duration() time.Duration {
	return time.Duration((r.EndEpoch - r.StartEpoch) * float64(time.Second))
}

// stressSummary is written to summary.json in the scenario artifacts.
type stressSummary struct {
	RunName      string                 `json:"run_name"`
	Cores        int                    `json:"reserved_cores"`
	Workers      []string               `json:"reserved_workers"`
	ExpectedJobs int                    `json:"expected_jobs"`
	Submit       stressSubmitResult     `json:"submit"`
	SubmitSecs   float64                `json:"submit_seconds"`
	Jobs         stressJobStats         `json:"jobs"`
	Probes       stressProbeSummary     `json:"probes"`
	Scheduler    stressSchedulerSummary `json:"scheduler"`
}

// stressMonitor samples controller responsiveness in the background while a load runs.
// samples is written by the goroutine only and read after done is closed.
type stressMonitor struct {
	cancel  context.CancelFunc
	done    chan struct{}
	samples []stressSample
}

// Performance implements the performance scenarios; its load is the scheduling stress burst.
type Performance struct {
	info    *framework.ClusterInfo
	runtime framework.Runtime
	kubectl *framework.KubectlClient

	// runName names one scenario run:
	// - the name of the reservation holding the cores the load runs on,
	// - the --job-name of every job.
	// So squeue, sacct and scancel select the run's jobs by name.
	runName string
	cores   int
	// reservedWorkers are the idle nodes picked for the reservation: the only
	// nodes the load runs on, and the only ones the scenario prepares and cleans.
	reservedWorkers []framework.WorkerInfo
	// artifacts are the scenario artifact paths, nil when the run keeps no artifacts.
	artifacts *framework.ScenarioArtifactPaths
	// workDir is the jail directory owned by the stress user where the submission scripts write.
	workDir string

	expectedJobs int
	submitResult stressSubmitResult
	jobs         stressJobStats
	sdiagBefore  sdiagStatistics
	monitor      *stressMonitor
	probes       stressProbeSummary
	scheduler    stressSchedulerSummary
}

func NewPerformance(info *framework.ClusterInfo, runtime framework.Runtime, kubectl *framework.KubectlClient) *Performance {
	return &Performance{info: info, runtime: runtime, kubectl: kubectl}
}

func (s *Performance) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the Slurm queue is idle$`, s.slurmQueueIsIdle)
	sc.Step(`^(\d+) free cores on (\d+) workers are reserved for the stress test user$`, s.coresAreReserved)
	sc.Step(`^the stress test user submits a burst of (\d+) jobs with 1 to (\d+) CPUs sleeping up to (\d+) seconds$`, s.submitBurst)
	sc.Step(`^every job in the burst is accepted within (\d+) minutes$`, s.burstIsAccepted)
	sc.Step(`^every job in the burst completes successfully within (\d+) minutes$`, s.burstCompletes)
	sc.Step(`^the node-side phases of the jobs stay within$`, s.jobPhasesWithinBudget)
	sc.Step(`^the controller stays responsive throughout the load within$`, s.controllerStaysResponsive)
	sc.Step(`^the scheduler statistics for the load stay within$`, s.schedulerStatisticsWithinBudget)
}

func (s *Performance) CleanupAndReset(ctx context.Context) {
	const (
		stressCleanupTimeout  = 2 * time.Minute
		stressCleanupPoll     = 5 * time.Second
		stressCleanupAttempts = 3
	)

	s.stopMonitor()
	cleanupCtx, cancel := context.WithTimeout(ctx, stressCleanupTimeout)
	defer cancel()
	if s.runName != "" {
		s.saveSummary()
		scancel := fmt.Sprintf("scancel --user=%s --name=%s", framework.ShellQuote(stressUserName), framework.ShellQuote(s.runName))
		if _, err := s.runtime.Jail().Run(cleanupCtx, scancel); err != nil {
			s.runtime.Logf("Cleanup: cancel stress jobs named %s: %v", s.runName, err)
		}
		// A reservation cannot be deleted while cancelled jobs are still completing in it.
		queueCommand := stressQueueStatesCommand(s.runName)
		if err := s.runtime.WaitFor(cleanupCtx, "cancelled stress jobs to leave the queue", stressCleanupTimeout, stressCleanupPoll,
			func(waitCtx context.Context) (bool, error) {
				output, err := s.runtime.Jail().Run(waitCtx, queueCommand)
				if err != nil {
					return false, err
				}
				return stressQueueDrained(output)
			}); err != nil {
			s.runtime.Logf("Cleanup: %v", err)
		}
		if _, err := s.runtime.Jail().RunWithRetry(cleanupCtx, stressDeleteReservationCommand(s.runName),
			stressCleanupAttempts, stressCleanupPoll); err != nil {
			s.runtime.Logf("Cleanup: delete stress reservation %s: %v", s.runName, err)
		}
		if err := s.dropWorkerCaches(cleanupCtx); err != nil {
			s.runtime.Logf("Cleanup: %v", err)
		}
	}
	*s = Performance{info: s.info, runtime: s.runtime, kubectl: s.kubectl}
}

// dropWorkerCaches releases the memory cgroups of finished jobs on the reserved workers.
// The epilog's drop_page_cache check does this after every ordinary job, but the stress jobs skip the checks,
// so their cgroups linger as dying cgroups holding per-CPU kernel state,
// and every new slurmstepd start gets slower as they pile up (150k of them made job startup p99 34 s instead of 2 s).
// Dropping the page cache lets the kernel reclaim them within seconds, so it runs before the load for a
// known starting point and after it so later scenarios are not slowed down.
func (s *Performance) dropWorkerCaches(ctx context.Context) error {
	for _, worker := range s.reservedWorkers {
		if _, err := s.runtime.Worker(worker).RunWithDefaultRetry(ctx, "sync && echo 3 > /proc/sys/vm/drop_caches"); err != nil {
			return fmt.Errorf("drop caches on %s: %w", worker.Name, err)
		}
	}
	return nil
}

// Steps, in scenario order.

func (s *Performance) slurmQueueIsIdle(ctx context.Context) error {
	cluster, err := s.kubectl.SlurmCluster(ctx, s.info.SlurmClusterName)
	if err != nil {
		return err
	}
	if !cluster.AccountingEnabled {
		s.runtime.Logf("Performance: Slurm accounting is disabled, skipping scenario")
		return godog.ErrSkip
	}

	if _, err := s.runtime.Jail().RunWithDefaultRetry(ctx, cancelStressUserJobsCommand()); err != nil {
		return fmt.Errorf("cancel leftover stress jobs: %w", err)
	}
	const listedRows = 10
	return s.runtime.WaitFor(ctx, "Slurm queue to be idle", stressIdleQueueTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			output, err := s.runtime.Jail().RunWithDefaultRetry(waitCtx, "squeue --noheader --format='%i|%u|%T|%j'")
			if err != nil {
				return false, fmt.Errorf("list Slurm queue: %w", err)
			}
			output = strings.TrimSpace(output)
			if output == "" {
				return true, nil
			}
			rows := strings.Split(output, "\n")
			return false, fmt.Errorf("Slurm queue holds %d jobs:\n%s", len(rows), strings.Join(rows[:min(listedRows, len(rows))], "\n"))
		})
}

// coresAreReserved pins the load to a fixed amount of cores so that the budgets mean the same thing on any cluster.
// Slurm packs a reservation's cores onto as few of its nodes as possible,
// so the split between the nodes varies with their size while the total does not.
// The step skips when the partition does not have enough nodes large enough for an even share,
// and fails when it does but the cores cannot be reserved, for example because a node is down or busy.
func (s *Performance) coresAreReserved(ctx context.Context, cores, nodes int) error {
	if nodes < 1 || cores < nodes {
		return fmt.Errorf("invalid reservation shape: %d cores on %d nodes", cores, nodes)
	}
	sinfoOutput, err := s.runtime.Jail().RunWithDefaultRetry(ctx, partitionNodesCommand(framework.MainPartitionName))
	if err != nil {
		return fmt.Errorf("list %s partition nodes: %w", framework.MainPartitionName, err)
	}
	partitionNodes, err := parsePartitionNodes(sinfoOutput)
	if err != nil {
		return err
	}
	if len(partitionNodes) == 0 {
		return fmt.Errorf("sinfo listed no nodes in partition %s", framework.MainPartitionName)
	}
	minCores := (cores + nodes - 1) / nodes
	chosen, err := chooseStressNodes(partitionNodes, nodes, minCores)
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		s.runtime.Logf("Performance: partition %s has fewer than %d nodes with %d cores, skipping scenario",
			framework.MainPartitionName, nodes, minCores)
		return godog.ErrSkip
	}

	// The reservation names its user, so slurmctld must already resolve the account,
	// and the chosen nodes must resolve it before the jobs start there.
	for _, worker := range chosen {
		s.reservedWorkers = append(s.reservedWorkers, framework.WorkerInfo{Name: worker})
	}
	if err := prepareSSHTestUser(ctx, s.runtime, stressUserName, s.reservedWorkers...); err != nil {
		return err
	}
	if err := s.dropWorkerCaches(ctx); err != nil {
		return err
	}

	if err := s.deleteStaleReservations(ctx); err != nil {
		return err
	}
	name := fmt.Sprintf("e2e-stress-%d", time.Now().UnixNano())
	s.runName = name
	s.cores = cores
	// The show after the create decides success, so a retry after a lost exec
	// stream finds the reservation already created instead of failing.
	command := stressReservationCommand(name, chosen, cores, stressReservationDuration) +
		"; scontrol show reservation " + framework.ShellQuote(name) + " --oneliner"
	shown, err := s.runtime.Jail().RunWithDefaultRetry(ctx, command)
	if err != nil {
		return fmt.Errorf("reserve %d cores on %s: %w", cores, strings.Join(chosen, ", "), err)
	}
	if got := extractField(shown, "CoreCnt"); got != strconv.Itoa(cores) {
		return fmt.Errorf("reservation %s holds %q cores, expected %d: %s", name, got, cores, strings.TrimSpace(shown))
	}
	s.runtime.Logf("Performance: reservation %s holds %d cores for %s: %s",
		name, cores, stressReservationDuration, strings.Join(reservationCoreSplit(shown), ", "))
	return nil
}

func (s *Performance) submitBurst(ctx context.Context, jobs, maxCPUs, maxSleepSeconds int) error {
	if jobs < 1 || maxCPUs < 1 || maxSleepSeconds < 1 {
		return fmt.Errorf("invalid burst shape: %d jobs, up to %d CPUs, up to %d seconds", jobs, maxCPUs, maxSleepSeconds)
	}
	s.expectedJobs = jobs

	s.workDir = path.Join(framework.AcceptanceJobOutputDir, s.runName)
	if artifacts, ok := framework.ScenarioArtifacts(ctx); ok {
		s.artifacts = &artifacts
		s.workDir = artifacts.JailDir
	}
	if _, err := s.runtime.Jail().RunWithDefaultRetry(ctx, userOwnedDirCommand(stressUserName, s.workDir)); err != nil {
		return fmt.Errorf("create stress work directory %s: %w", s.workDir, err)
	}

	before, err := s.readSdiag(ctx, "sdiag-before.json")
	if err != nil {
		return err
	}
	s.sdiagBefore = before
	s.startMonitor(ctx)

	script := stressBurstScript(s.workDir, s.runName, jobs, maxCPUs, maxSleepSeconds)
	s.runtime.Logf("Performance: submitting %d jobs named %s", jobs, s.runName)
	// Not retried: a second pass would submit the jobs again.
	output, err := s.runtime.Jail().Run(ctx, runAsLoginUser(stressUserName, framework.BashLC(script)))
	if err != nil {
		return fmt.Errorf("run submission as %s: %w%s", stressUserName, err, s.submissionErrorTail(ctx))
	}
	result, err := parseStressSubmitResult(output)
	if err != nil {
		return fmt.Errorf("parse submission result: %w", err)
	}
	s.submitResult = result
	s.runtime.Logf("Performance: submission accepted %d of %d in %s (status %d)",
		result.Accepted, jobs, result.Duration().Round(time.Millisecond), result.ExitStatus)
	return nil
}

func (s *Performance) burstIsAccepted(ctx context.Context, minutes int) error {
	if err := evaluateStressSubmit(s.submitResult, s.expectedJobs, time.Duration(minutes)*time.Minute); err != nil {
		return fmt.Errorf("%w%s", err, s.submissionErrorTail(ctx))
	}
	return nil
}

func (s *Performance) burstCompletes(ctx context.Context, minutes int) error {
	stats, err := s.collectCompletedJobs(ctx)
	if err != nil {
		return err
	}
	return evaluateStressCompletion(stats, stressCompletionBudget{
		Wall:       time.Duration(minutes) * time.Minute,
		MinWorkers: len(s.reservedWorkers),
	})
}

func (s *Performance) jobPhasesWithinBudget(_ context.Context, table *godog.Table) error {
	values, err := parseKeyValueTable(table)
	if err != nil {
		return err
	}
	budget := stressPhaseBudget{
		StartupP99:  values.duration("startup p99"),
		StepsP99:    values.duration("steps p99"),
		TeardownP99: values.duration("teardown p99"),
	}
	if err := values.done(); err != nil {
		return err
	}
	return evaluateStressJobPhases(s.jobs, budget)
}

func (s *Performance) controllerStaysResponsive(_ context.Context, table *godog.Table) error {
	values, err := parseKeyValueTable(table)
	if err != nil {
		return err
	}
	budget := stressProbeBudget{
		SqueueMean: values.duration("squeue mean"),
		SqueueMax:  values.duration("squeue max"),
		SinfoMax:   values.duration("sinfo max"),
		SdiagMax:   values.duration("sdiag max"),
		MinSamples: 3,
	}
	if err := values.done(); err != nil {
		return err
	}

	s.stopMonitor()
	samples := s.monitor.samples
	summary := summarizeStressProbes(samples)
	s.probes = summary
	s.runtime.Logf("Performance: %d probes (%d failed to run), squeue %s, sinfo %s, sdiag %s, pending max %d, running max %d",
		summary.Samples, summary.Errors, summary.Squeue.format("ms"), summary.Sinfo.format("ms"), summary.Sdiag.format("ms"),
		summary.PendingMax, summary.RunningMax)
	for _, sample := range samples {
		at := sample.At.Format(time.TimeOnly)
		switch {
		case sample.Error != "":
			s.runtime.Logf("Performance: probe at %s failed to run: %s", at, sample.Error)
		case sample.SqueueMillis > budget.SqueueMax.Milliseconds() || sample.SinfoMillis > budget.SinfoMax.Milliseconds() ||
			sample.SdiagMillis > budget.SdiagMax.Milliseconds():
			s.runtime.Logf("Performance: probe at %s: squeue %d ms, sinfo %d ms, sdiag %d ms",
				at, sample.SqueueMillis, sample.SinfoMillis, sample.SdiagMillis)
		}
	}
	return evaluateStressProbes(summary, budget)
}

func (s *Performance) schedulerStatisticsWithinBudget(ctx context.Context, table *godog.Table) error {
	values, err := parseKeyValueTable(table)
	if err != nil {
		return err
	}
	budget := stressSchedulerBudget{
		MinSubmitted:      int64(s.expectedJobs),
		ScheduleCycleMean: values.duration("main scheduler cycle mean"),
		MaxServerThreads:  values.count("server threads at the end"),
	}
	if err := values.done(); err != nil {
		return err
	}
	after, err := s.readSdiag(ctx, "sdiag-after.json")
	if err != nil {
		return err
	}
	snapshots := []sdiagStatistics{s.sdiagBefore}
	for _, sample := range s.monitor.samples {
		if sample.Sdiag != nil {
			snapshots = append(snapshots, *sample.Sdiag)
		}
	}
	summary := summarizeStressScheduler(append(snapshots, after))
	s.scheduler = summary
	s.runtime.Logf("Performance: scheduler submitted %d, failed %d, canceled %d, %d cycles with mean %.3fs, agent queue %d, threads %d (resets=%d)",
		summary.JobsSubmitted, summary.JobsFailed, summary.JobsCanceled, summary.ScheduleCycles,
		summary.ScheduleCycleMeanSecs, summary.AgentQueueSize, summary.ServerThreadCount, summary.Resets)
	return evaluateStressScheduler(summary, budget)
}

// Helpers.

// deleteStaleReservations removes reservations left by a run that lost its cleanup;
// they would keep the nodes from being idle.
func (s *Performance) deleteStaleReservations(ctx context.Context) error {
	output, err := s.runtime.Jail().RunWithDefaultRetry(ctx, "scontrol show reservations --oneliner 2>/dev/null || true")
	if err != nil {
		return fmt.Errorf("list reservations: %w", err)
	}
	for _, name := range staleStressReservations(output) {
		s.runtime.Logf("Performance: deleting stale reservation %s", name)
		if _, err := s.runtime.Jail().RunWithDefaultRetry(ctx, stressDeleteReservationCommand(name)); err != nil {
			return fmt.Errorf("delete stale reservation %s: %w", name, err)
		}
	}
	return nil
}

// collectCompletedJobs waits for the load to leave the queue and for accounting to hold every job,
// then reads the full records once, saves them and returns the computed statistics.
func (s *Performance) collectCompletedJobs(ctx context.Context) (stressJobStats, error) {
	queueCommand := stressQueueStatesCommand(s.runName)
	if err := s.runtime.WaitFor(ctx, fmt.Sprintf("jobs %s to leave the queue", s.runName),
		stressDrainTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			output, err := s.runtime.Jail().RunWithDefaultRetry(waitCtx, queueCommand)
			if err != nil {
				return false, fmt.Errorf("list jobs %s: %w", s.runName, err)
			}
			return stressQueueDrained(output)
		}); err != nil {
		return stressJobStats{}, err
	}

	const accountingTimeout = 2 * time.Minute
	countCommand := stressSacctCountCommand(s.runName)
	if err := s.runtime.WaitFor(ctx, fmt.Sprintf("accounting records of %d jobs", s.expectedJobs),
		accountingTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			output, err := s.runtime.Jail().RunWithDefaultRetry(waitCtx, countCommand)
			if err != nil {
				return false, fmt.Errorf("count accounting records of %s: %w", s.runName, err)
			}
			return stressAccountingSettled(output, s.expectedJobs)
		}); err != nil {
		return stressJobStats{}, err
	}

	raw, err := s.runtime.Jail().RunWithDefaultRetry(ctx, stressSacctCommand(s.runName))
	if err != nil {
		return stressJobStats{}, fmt.Errorf("query accounting for %s: %w", s.runName, err)
	}
	s.saveArtifact("jobs.csv", raw)
	records, err := parseStressJobRecords(raw)
	if err != nil {
		return stressJobStats{}, err
	}
	if len(records) != s.expectedJobs {
		return stressJobStats{}, fmt.Errorf("expected %d accounting records, got %d", s.expectedJobs, len(records))
	}
	if err := stressJobProblems(records); err != nil {
		return stressJobStats{}, err
	}
	stats := computeStressJobStats(records)
	s.jobs = stats
	s.runtime.Logf("Performance: %d jobs completed in %.1fs (%.1f jobs/s) on %d workers; queue wait %s; startup %s; steps %s; teardown %s",
		stats.Jobs, stats.WallSeconds, stats.ThroughputPerSecond, len(stats.Workers),
		stats.QueueWait, stats.Startup, stats.Steps, stats.Teardown)
	return stats, nil
}

// readSdiag takes a controller statistics snapshot and saves the raw JSON as an artifact.
func (s *Performance) readSdiag(ctx context.Context, artifact string) (sdiagStatistics, error) {
	output, err := s.runtime.Jail().RunWithDefaultRetry(ctx, "sdiag --json")
	if err != nil {
		return sdiagStatistics{}, fmt.Errorf("read sdiag: %w", err)
	}
	s.saveArtifact(artifact, output)
	return parseSdiag(output)
}

const stressMonitorInterval = 10 * time.Second

func (s *Performance) startMonitor(ctx context.Context) {
	monitorCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	monitor := &stressMonitor{cancel: cancel, done: make(chan struct{})}
	s.monitor = monitor
	jail := s.runtime.Jail()
	go func() {
		defer close(monitor.done)
		ticker := time.NewTicker(stressMonitorInterval)
		defer ticker.Stop()
		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-ticker.C:
			}
			output, err := jail.Run(monitorCtx, stressProbeCommand())
			if monitorCtx.Err() != nil {
				return
			}
			sample := parseStressSample(output)
			sample.At = time.Now()
			if err != nil {
				sample.Error = err.Error()
			}
			monitor.samples = append(monitor.samples, sample)
		}
	}()
}

// stopMonitor ends the probes, waits for the goroutine and saves the samples;
// it is a no-op once the monitor is stopped.
func (s *Performance) stopMonitor() {
	if s.monitor == nil || s.monitor.cancel == nil {
		return
	}
	s.monitor.cancel()
	s.monitor.cancel = nil
	<-s.monitor.done
	s.saveSamples(s.monitor.samples)
}

func (s *Performance) saveSamples(samples []stressSample) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	for _, sample := range samples {
		if err := encoder.Encode(sample); err != nil {
			s.runtime.Logf("Encode stress sample: %v", err)
		}
	}
	s.saveArtifact("samples.jsonl", buffer.String())
}

// saveSummary writes summary.json with whatever the steps so far have measured.
func (s *Performance) saveSummary() {
	summary := stressSummary{
		RunName:      s.runName,
		Cores:        s.cores,
		Workers:      framework.WorkerNames(s.reservedWorkers),
		ExpectedJobs: s.expectedJobs,
		Submit:       s.submitResult,
		SubmitSecs:   s.submitResult.Duration().Seconds(),
		Jobs:         s.jobs,
		Probes:       s.probes,
		Scheduler:    s.scheduler,
	}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		s.runtime.Logf("Encode stress summary: %v", err)
		return
	}
	s.saveArtifact("summary.json", string(encoded)+"\n")
}

// saveArtifact writes content into the scenario runner artifacts; it is a no-op without artifacts.
func (s *Performance) saveArtifact(name, content string) {
	if s.artifacts == nil {
		return
	}
	if err := s.artifacts.WriteRunnerFile(name, content); err != nil {
		s.runtime.Logf("Stress artifact: %v", err)
	}
}

// submissionErrorTail returns the end of the submission stderr for failure messages.
func (s *Performance) submissionErrorTail(ctx context.Context) string {
	if s.workDir == "" {
		return ""
	}
	const tailLines = 20
	tailCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	command := fmt.Sprintf("tail --lines=%d %s 2>&1 || true", tailLines, framework.ShellQuote(path.Join(s.workDir, "sbatch.err")))
	output, err := s.runtime.Jail().Run(tailCtx, command)
	if err != nil {
		return fmt.Sprintf("\nsbatch.err: %v", err)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return "\nsbatch.err: empty"
	}
	return "\nsbatch.err:\n" + output
}

// stressQueueStatesCommand counts the queued jobs of the load by state inside the
// jail, in the same `uniq --count` form as stressSacctCountCommand; the output is
// empty once the queue holds none of them.
func stressQueueStatesCommand(jobName string) string {
	return "squeue --noheader --name=" + framework.ShellQuote(jobName) + " --format='%T' | sort | uniq --count"
}

// joinProblems turns the problems an evaluation found into one error, or nil without any.
func joinProblems(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// stressQueueDrained reads the queue states command output and reports whether no job of the load is queued.
func stressQueueDrained(output string) (bool, error) {
	counts := parseStateCounts(output)
	if len(counts) == 0 {
		return true, nil
	}
	return false, fmt.Errorf("%d jobs still queued: %s", sumCounts(counts), formatStateCounts(counts))
}

// parseStateCounts reads `uniq -c` style lines of the form "<count> <state>" and ignores every other line.
func parseStateCounts(output string) map[string]int {
	counts := map[string]int{}
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		count, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		counts[fields[1]] += count
	}
	return counts
}

func sumCounts(counts map[string]int) int {
	var total int
	for _, count := range counts {
		total += count
	}
	return total
}

func formatStateCounts(counts map[string]int) string {
	parts := make([]string, 0, len(counts))
	for state, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", state, count))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// staleStressReservations returns the names of this scenario's reservations in
// `scontrol show reservations -o` output.
func staleStressReservations(output string) []string {
	var names []string
	for line := range strings.SplitSeq(output, "\n") {
		if name := extractField(line, "ReservationName"); strings.HasPrefix(name, "e2e-stress-") {
			names = append(names, name)
		}
	}
	return names
}

// cancelStressUserJobsCommand cancels the jobs of the stress user, which can only be leftovers of a run that
// lost its cleanup. On a fresh cluster the user does not exist yet and scancel would reject the name.
func cancelStressUserJobsCommand() string {
	user := framework.ShellQuote(stressUserName)
	return fmt.Sprintf("! id %s >/dev/null 2>&1 || scancel --user=%s", user, user)
}

// chooseStressNodes picks count idle nodes with at least `cores` cores, by name.
// It returns nil when the partition has fewer than count nodes of that size at all (the scenario skips),
// and an error when it has them but fewer are idle.
func chooseStressNodes(nodes []partitionNode, count, cores int) ([]string, error) {
	var large []partitionNode
	for _, node := range nodes {
		if node.Cores >= cores {
			large = append(large, node)
		}
	}
	if len(large) < count {
		return nil, nil
	}
	var chosen, busy []string
	for _, node := range large {
		if node.State == "idle" {
			chosen = append(chosen, node.Name)
			continue
		}
		busy = append(busy, node.Name+"="+node.State)
	}
	if len(chosen) < count {
		return nil, fmt.Errorf("only %d of %d nodes with at least %d cores are idle, need %d: %s",
			len(chosen), len(large), cores, count, strings.Join(busy, ", "))
	}
	return chosen[:count], nil
}

// stressReservationCommand reserves cores in total on the named nodes for the
// user, starting now. The nodes are explicit so that the step controls which idle
// nodes take part; Slurm decides how the cores are packed onto them.
func stressReservationCommand(name string, nodes []string, cores int, duration time.Duration) string {
	return framework.ShellJoin("scontrol", "create", "reservation",
		"ReservationName="+name,
		"Users="+stressUserName,
		"PartitionName="+framework.MainPartitionName,
		"StartTime=now",
		fmt.Sprintf("Duration=%d", int(duration.Minutes())),
		"Nodes="+strings.Join(nodes, ","),
		fmt.Sprintf("CoreCnt=%d", cores),
	)
}

// stressDeleteReservationCommand succeeds when the reservation is deleted or already gone,
// so a retry after a lost exec stream does not fail on the missing reservation.
func stressDeleteReservationCommand(name string) string {
	quoted := framework.ShellQuote(name)
	return "scontrol delete reservation " + quoted + " || ! scontrol show reservation " + quoted + " --oneliner >/dev/null 2>&1"
}

// reservationCoreSplit lists node=core-ids pairs from `scontrol show reservation -o`.
func reservationCoreSplit(output string) []string {
	var pairs []string
	var node string
	for _, field := range strings.Fields(output) {
		if value, found := strings.CutPrefix(field, "NodeName="); found {
			node = value
			continue
		}
		if value, found := strings.CutPrefix(field, "CoreIDs="); found && node != "" {
			pairs = append(pairs, node+"="+value)
			node = ""
		}
	}
	return pairs
}

// skipChecksComment is the sbatch comment the passive prolog/epilog checks recognize to skip a job.
const skipChecksComment = "skip_checks"

// stressSbatchTemplate is the sbatch command line of one stress job, for xargs.
func stressSbatchTemplate(runName string, maxSleepSeconds int) string {
	// A safety time cap; the jobs normally finish within seconds.
	const timeLimitMinutes = "5"
	return framework.ShellJoin(
		"sbatch", "--parsable",
		"--job-name="+runName,
		"--reservation="+runName,
		// Skips the prolog/epilog checks so the per-job cost is scheduling rather
		// than the checks; dropWorkerCaches makes up for the skipped cache drop.
		"--comment="+skipChecksComment,
		"--mem=256M",
		"--time="+timeLimitMinutes,
		"--output=/dev/null",
		"--error=/dev/null",
		"--cpus-per-task={}", // xargs substitutes the CPU count.
		// shuf draws the sleep because --wrap scripts run under /bin/sh, which has no RANDOM.
		fmt.Sprintf(`--wrap=srun sleep "$(shuf --input-range=1-%d --head-count=1)"`, maxSleepSeconds),
	)
}

// stressBurstScript submits the jobs individually from workDir and reports the
// loop's epoch bounds, its exit status and the accepted job count as key=value lines.
// The CPU request rotates through 1..maxCPUs instead of staying at 1:
// Soperator sets pack_serial_at_end in SchedulerParameters, which gives one-CPU single-node jobs their own packing
// path, so an all-serial burst would exercise only that special case and leave the general core selection untested.
func stressBurstScript(workDir, runName string, jobs, maxCPUs, maxSleepSeconds int) string {
	// Enough concurrent sbatch processes to keep the per-user RPC limiter saturated.
	const parallelism = 8
	return strings.Join([]string{
		"cd " + framework.ShellQuote(workDir) + " || exit 1",
		"rm --force ids.txt sbatch.err",
		"start=$(date +%s.%N)",
		fmt.Sprintf("seq 1 %d | awk '{print ($1 %% %d) + 1}' | xargs --max-procs=%d --replace={} %s >> ids.txt 2>> sbatch.err",
			jobs, maxCPUs, parallelism, stressSbatchTemplate(runName, maxSleepSeconds)),
		"status=$?",
		"end=$(date +%s.%N)",
		"echo start=$start",
		"echo end=$end",
		"echo status=$status",
		"echo accepted=$(cat ids.txt 2>/dev/null | wc --lines)",
	}, "\n")
}

func parseStressSubmitResult(output string) (stressSubmitResult, error) {
	values := parseKeyValues(output)
	field := func(key string) (string, error) {
		value, ok := values[key]
		if !ok {
			return "", fmt.Errorf("submission output lacks %s:\n%s", key, strings.TrimSpace(output))
		}
		return value, nil
	}
	number := func(key string) (float64, error) {
		value, err := field(key)
		if err != nil {
			return 0, err
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, fmt.Errorf("parse submission %s %q: %w", key, value, err)
		}
		return parsed, nil
	}
	var result stressSubmitResult
	var err error
	if result.StartEpoch, err = number("start"); err != nil {
		return stressSubmitResult{}, err
	}
	if result.EndEpoch, err = number("end"); err != nil {
		return stressSubmitResult{}, err
	}
	status, err := number("status")
	if err != nil {
		return stressSubmitResult{}, err
	}
	accepted, err := number("accepted")
	if err != nil {
		return stressSubmitResult{}, err
	}
	result.ExitStatus, result.Accepted = int(status), int(accepted)
	return result, nil
}

func evaluateStressSubmit(result stressSubmitResult, expected int, budget time.Duration) error {
	var problems []string
	if result.Accepted != expected {
		problems = append(problems, fmt.Sprintf("accepted %d of %d submissions", result.Accepted, expected))
	}
	if result.ExitStatus != 0 {
		problems = append(problems, fmt.Sprintf("submission loop exited with status %d", result.ExitStatus))
	}
	if duration := result.Duration(); duration > budget {
		problems = append(problems, fmt.Sprintf("submission took %s, budget %s", duration.Round(time.Millisecond), budget))
	}
	return joinProblems(problems)
}
