package sharedsteps

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type ncclDebugStressScopeKey struct{}

type NCCLDebugStress struct {
	runtime       framework.Runtime
	slurm         *framework.SlurmClient
	kubectl       *framework.KubectlClient
	selector      *framework.WorkerSelector
	worker        framework.WorkerInfo
	pod           framework.WorkerPodInfo
	dir           string
	nativeJobs    []framework.SbatchJob
	containerJobs []framework.SbatchJob
	startsPerJob  int
	outputs       map[string]string
}

type ncclDebugContainerResults struct {
	starts, failed, markers int
}

func NewNCCLDebugStress(runtime framework.Runtime, slurm *framework.SlurmClient, kubectl *framework.KubectlClient, selector *framework.WorkerSelector) *NCCLDebugStress {
	return &NCCLDebugStress{runtime: runtime, slurm: slurm, kubectl: kubectl, selector: selector}
}

func (s *NCCLDebugStress) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a worker is selected for concurrent container logging$`, s.selectWorker)
	sc.Step(`^(\d+) background jobs repeatedly run short native steps with NCCL logging$`, s.startNativeJobs)
	sc.Step(`^(\d+) batch jobs each start (\d+) Ubuntu containers with NCCL logging on the same worker$`, s.startContainerJobs)
	sc.Step(`^the background jobs are stopped after all container attempts finish$`, s.stopNativeJobs)
	sc.Step(`^all (\d+) container starts succeed$`, s.checkContainerStarts)
	sc.Step(`^every container's log marker is readable outside the container$`, s.checkLogMarkers)
	sc.Step(`^the background jobs report no failed steps$`, s.checkNativeJobs)
	// The shared suite has not enabled native reservations for every scenario yet.
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if ctx.Value(ncclDebugStressScopeKey{}) != nil {
			s.CleanupAndReset(context.WithoutCancel(ctx))
			return ctx, framework.FinishWorkerReservation(ctx, s.runtime)
		}
		return ctx, nil
	})
}

func (s *NCCLDebugStress) selectWorker(ctx context.Context) (context.Context, error) {
	ctx = context.WithValue(ctx, ncclDebugStressScopeKey{}, true)
	ctx = framework.WithWorkerReservation(ctx, framework.NewReservationManager(), "NCCL debug container starts")
	workers, err := s.selector.Acquire(ctx, framework.WorkerRequirements{Count: 1, Idle: true})
	if err != nil {
		return ctx, err
	}
	s.worker = workers[0]
	s.pod, err = s.kubectl.WorkerPodForSlurmNode(ctx, s.worker.Name)
	return ctx, err
}

func (s *NCCLDebugStress) startNativeJobs(ctx context.Context, count int) error {
	artifacts, ok := framework.ScenarioArtifacts(ctx)
	if !ok {
		return fmt.Errorf("prepare concurrent logging: scenario artifact paths are unavailable")
	}
	template := path.Join(artifacts.JailDir, "container-starts-XXXXXX")
	directory, err := s.runtime.Jail().Run(ctx, "mktemp -d "+framework.ShellQuote(template))
	if err != nil {
		return err
	}
	s.dir = strings.TrimSpace(directory)
	if !strings.HasPrefix(s.dir, strings.TrimSuffix(template, "XXXXXX")) || path.Clean(s.dir) != s.dir {
		return fmt.Errorf("parse concurrent logging directory from %q", directory)
	}
	if _, err := s.runtime.Jail().Run(ctx, userOwnedDirCommand(soperatorChecksUserName, s.dir)); err != nil {
		return err
	}
	for name, script := range map[string]string{"task.sh": ncclDebugStressTask(), "native.sh": ncclDebugNativeBatch(s.dir)} {
		if err := s.writeScript(ctx, name, script); err != nil {
			return err
		}
	}
	s.nativeJobs, err = s.submitJobs(ctx, "native", count)
	if err != nil {
		return err
	}
	if err := s.waitForFiles(ctx, "ready", count); err != nil {
		return err
	}
	if err := s.release(ctx, "start-native"); err != nil {
		return err
	}
	return s.waitForFiles(ctx, "churning", count)
}

func (s *NCCLDebugStress) startContainerJobs(ctx context.Context, count, startsPerJob int) error {
	if startsPerJob < 1 {
		return fmt.Errorf("start container jobs: starts per job must be positive")
	}
	s.startsPerJob = startsPerJob
	if err := s.writeScript(ctx, "container.sh", ncclDebugContainerBatch(s.dir, startsPerJob)); err != nil {
		return err
	}
	var err error
	s.containerJobs, err = s.submitJobs(ctx, "container", count)
	if err != nil {
		return err
	}
	if err := s.waitForFiles(ctx, "ready", len(s.nativeJobs)+len(s.containerJobs)); err != nil {
		return err
	}
	return s.release(ctx, "start-container")
}

func (s *NCCLDebugStress) stopNativeJobs(ctx context.Context) error {
	if err := s.waitForFiles(ctx, "done", len(s.containerJobs)); err != nil {
		return err
	}
	if err := s.release(ctx, "stop"); err != nil {
		return err
	}
	for _, job := range slices.Concat(s.containerJobs, s.nativeJobs) {
		if err := s.slurm.WaitForJobGone(ctx, job.ID, time.Minute); err != nil {
			return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, job, err)
		}
	}
	return s.collectResults(ctx)
}

func (s *NCCLDebugStress) submitJobs(ctx context.Context, kind string, count int) ([]framework.SbatchJob, error) {
	if count < 1 {
		return nil, fmt.Errorf("submit %s jobs: count must be positive", kind)
	}
	var jobs []framework.SbatchJob
	for range count {
		job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
			JobName: "e2e-nccl-debug-" + kind, Nodes: 1, Nodelist: []string{s.worker.Name}, TasksPerNode: 1,
			Args:      []string{"--cpus-per-task=1", "--mem=512M", "--export=ALL,SNCCLD_ENABLED=0"},
			TimeLimit: 5 * time.Minute, RunAsUser: soperatorChecksUserName,
			Wrap: "/bin/bash " + framework.ShellQuote(path.Join(s.dir, kind+".sh")),
		})
		if err != nil {
			return jobs, err
		}
		jobs = append(jobs, job)
		s.runtime.Logf("Concurrent logging: worker=%s kind=%s job=%s", s.worker.Name, kind, job.ID)
	}
	return jobs, nil
}

func (s *NCCLDebugStress) writeScript(ctx context.Context, name, script string) error {
	command := "printf '%s' " + framework.ShellQuote(base64.StdEncoding.EncodeToString([]byte(script))) +
		" | base64 --decode > " + framework.ShellQuote(path.Join(s.dir, name))
	_, err := s.runtime.Jail().Run(ctx, command)
	return err
}

func (s *NCCLDebugStress) release(ctx context.Context, name string) error {
	_, err := s.runtime.Jail().Run(ctx, "touch -- "+framework.ShellQuote(path.Join(s.dir, name)))
	return err
}

func (s *NCCLDebugStress) waitForFiles(ctx context.Context, suffix string, count int) error {
	return s.runtime.WaitFor(ctx, "concurrent logging "+suffix, 3*time.Minute, time.Second, func(ctx context.Context) (bool, error) {
		output, err := s.runtime.Jail().Run(ctx, "find "+framework.ShellQuote(s.dir)+" -maxdepth 1 -name "+framework.ShellQuote("*."+suffix)+" -printf '.\\n' | wc -l")
		if err != nil {
			return false, err
		}
		found, err := strconv.Atoi(strings.TrimSpace(output))
		if err != nil || found != count {
			return false, fmt.Errorf("wait for %d %s files on %s: got %q", count, suffix, s.worker.Name, output)
		}
		return true, nil
	})
}

func (s *NCCLDebugStress) collectResults(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	artifacts, ok := framework.ScenarioArtifacts(ctx)
	if !ok {
		return fmt.Errorf("collect concurrent logging: scenario artifact paths are unavailable")
	}
	if s.outputs == nil {
		s.outputs = make(map[string]string)
	}
	var failures []error
	for _, job := range slices.Concat(s.containerJobs, s.nativeJobs) {
		if _, collected := s.outputs[job.ID]; collected {
			continue
		}
		stdout, outErr := readJobFile(ctx, s.runtime, job.StdoutPath)
		stderr, errErr := readJobFile(ctx, s.runtime, job.StderrPath)
		err := errors.Join(outErr, errErr,
			artifacts.WriteRunnerFile(job.ID+"-stdout.txt", stdout),
			artifacts.WriteRunnerFile(job.ID+"-stderr.txt", stderr))
		if err != nil {
			failures = append(failures, err)
			continue
		}
		s.outputs[job.ID] = stdout
	}
	return errors.Join(failures...)
}

func (s *NCCLDebugStress) checkContainerStarts(expected int) error {
	if total := len(s.containerJobs) * s.startsPerJob; expected < 1 || total != expected {
		return fmt.Errorf("verify container count: expected %d starts, submitted %d", expected, total)
	}
	var failures []error
	for _, job := range s.containerJobs {
		results, err := parseNCCLDebugContainerResults(s.outputs[job.ID], s.worker.Name, s.startsPerJob)
		if err != nil {
			failures = append(failures, fmt.Errorf("job %s: %w", job.ID, err))
		} else if results.failed != 0 {
			failures = append(failures, fmt.Errorf("job %s: %d/%d container starts failed", job.ID, results.failed, results.starts))
		}
	}
	return errors.Join(failures...)
}

func (s *NCCLDebugStress) checkLogMarkers(ctx context.Context) error {
	for _, job := range s.containerJobs {
		results, err := parseNCCLDebugContainerResults(s.outputs[job.ID], s.worker.Name, s.startsPerJob)
		if err != nil {
			return err
		}
		if results.markers != results.starts {
			return fmt.Errorf("job %s: verified %d/%d container log markers", job.ID, results.markers, results.starts)
		}
	}
	for _, job := range s.containerJobs {
		if err := framework.WaitForJobSucceeded(ctx, s.runtime, s.slurm, job, time.Minute); err != nil {
			return err
		}
	}
	return nil
}

func (s *NCCLDebugStress) checkNativeJobs(ctx context.Context) error {
	var nativeSteps int
	var ids []string
	for _, job := range s.nativeJobs {
		steps, err := validateNCCLDebugNativeResults(s.outputs[job.ID], s.worker.Name)
		if err != nil {
			return fmt.Errorf("job %s: %w", job.ID, err)
		}
		if err := framework.WaitForJobSucceeded(ctx, s.runtime, s.slurm, job, time.Minute); err != nil {
			return err
		}
		nativeSteps += steps
	}
	for _, job := range slices.Concat(s.containerJobs, s.nativeJobs) {
		ids = append(ids, job.ID)
	}
	total := len(s.containerJobs) * s.startsPerJob
	summary := fmt.Sprintf("worker=%s jobs=%s\n%d/%d container starts succeeded; %d/%d log markers verified\n%d native steps; 0 failures\n",
		s.worker.Name, strings.Join(ids, ","), total, total, total, total, nativeSteps)
	s.runtime.Logf("Concurrent logging: %s", summary)
	artifacts, _ := framework.ScenarioArtifacts(ctx)
	return artifacts.WriteRunnerFile("container-starts-summary.txt", summary)
}

func parseNCCLDebugContainerResults(output, worker string, expected int) (ncclDebugContainerResults, error) {
	var results ncclDebugContainerResults
	if expected < 1 || parseKeyValueLine(output, "BATCH_HOST") != worker {
		return results, fmt.Errorf("verify container batch on %s with %d starts: %q", worker, expected, output)
	}
	seen := make([]bool, expected)
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.HasPrefix(line, "RESULT\t") {
			continue
		}
		var index, rc, marker int
		n, err := fmt.Sscanf(line, "RESULT\t%d\t%d\t%d", &index, &rc, &marker)
		if err != nil || n != 3 || len(strings.Fields(line)) != 4 || index < 0 || index >= expected || seen[index] || rc < 0 || rc > 255 || marker < 0 || marker > 1 {
			return results, fmt.Errorf("parse container result %q", line)
		}
		seen[index] = true
		results.starts++
		if rc != 0 {
			results.failed++
		}
		results.markers += marker
	}
	if results.starts != expected {
		return results, fmt.Errorf("verify container results: expected %d starts, found %d", expected, results.starts)
	}
	return results, nil
}

func validateNCCLDebugNativeResults(output, worker string) (int, error) {
	if parseKeyValueLine(output, "BATCH_HOST") != worker {
		return 0, fmt.Errorf("verify native batch placement: expected %s, got %q", worker, output)
	}
	var summaries, steps int
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.HasPrefix(line, "CHURN\t") {
			continue
		}
		var failed int
		if n, err := fmt.Sscanf(line, "CHURN\t%d\t%d", &steps, &failed); err != nil || n != 2 || len(strings.Fields(line)) != 3 || steps < 2 || failed != 0 {
			return 0, fmt.Errorf("verify native churn: %q", line)
		}
		summaries++
	}
	if summaries != 1 {
		return 0, fmt.Errorf("verify native churn: expected one summary, found %d", summaries)
	}
	return steps, nil
}

func (s *NCCLDebugStress) CleanupAndReset(ctx context.Context) {
	defer func() {
		s.worker, s.pod, s.dir = framework.WorkerInfo{}, framework.WorkerPodInfo{}, ""
		s.nativeJobs, s.containerJobs, s.outputs, s.startsPerJob = nil, nil, nil, 0
	}()
	jobs := slices.Concat(s.containerJobs, s.nativeJobs)
	if len(jobs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, ncclDebugCleanup)
	defer cancel()
	for _, job := range jobs {
		if err := s.slurm.CancelJob(ctx, job.ID, ncclDebugCleanup); err != nil {
			framework.RecordWorkerCleanupError(ctx, err)
			s.runtime.Logf("Cleanup: cancel concurrent logging job %s: %v", job.ID, err)
			continue
		}
		if _, err := s.runtime.WorkerPod(s.pod).Run(ctx, ncclDebugRemoveResourcesCommand(job.ID)); err != nil {
			framework.RecordWorkerCleanupError(ctx, err)
			s.runtime.Logf("Cleanup: remove concurrent logging resources for job %s: %v", job.ID, err)
		}
	}
	if err := s.collectResults(ctx); err != nil {
		s.runtime.Logf("Cleanup: collect concurrent logging artifacts: %v", err)
	}
}

func ncclDebugStressTask() string {
	return `test "$(id -un)" = soperatorchecks
test "$(id -u)" -ne 0
test "$NCCL_DEBUG" = INFO
test "$NCCL_DEBUG_FILE" = ` + framework.ShellQuote(ncclDebugOutputDir) + `/"$(hostname).$SLURM_JOB_ID.$SLURM_STEP_ID.out"
token="e2e-nccl:$SLURM_JOB_ID:$SLURM_STEP_ID"
printf '%s\n' "$token" > "$NCCL_DEBUG_FILE"
printf 'TOKEN=%s\nLOG_PATH=%s\n' "$token" "$NCCL_DEBUG_FILE"
`
}

func ncclDebugStressBatchSetup(directory, kind string) string {
	return "set -uo pipefail\nunset NCCL_DEBUG NCCL_DEBUG_FILE NCCL_DEBUG_SUBSYS NCCL_DEBUG_LEVELS\n" +
		"directory=" + framework.ShellQuote(directory) + "\n" +
		"printf 'BATCH_HOST=%s\\n' \"$(hostname)\"\n" +
		"touch \"$directory/$SLURM_JOB_ID.ready\"\n" +
		framework.JobReleaseGate(path.Join(directory, "start-"+kind), 2*time.Minute) + " || exit $?\nfailures=0\n" +
		"launcher=(timeout 60 srun --nodes=1 --ntasks=1 --cpus-per-task=1 --exact --immediate=30 --nccld-enabled=1 --nccld-log-level=INFO --nccld-out-file=1 --nccld-out-stdout=0 --nccld-out-dir=" + framework.ShellQuote(ncclDebugOutputDir) + ")\n"
}

func ncclDebugNativeBatch(directory string) string {
	return ncclDebugStressBatchSetup(directory, "native") + `i=0
while [[ ! -f "$directory/stop" ]]; do
    "${launcher[@]}" /bin/sleep 0.15 > "$directory/$SLURM_JOB_ID.native.out" 2> "$directory/$SLURM_JOB_ID.native.err"
    rc=$?
    if ((rc != 0)); then cat "$directory/$SLURM_JOB_ID.native.err" >&2; failures=$((failures+1)); fi
    i=$((i+1))
    touch "$directory/$SLURM_JOB_ID.churning"
done
printf 'CHURN\t%d\t%d\n' "$i" "$failures"
test "$failures" -eq 0
`
}

func ncclDebugContainerBatch(directory string, starts int) string {
	return ncclDebugStressBatchSetup(directory, "container") + fmt.Sprintf("for ((i=0; i<%d; i++)); do\n", starts) + `
    out="$directory/$SLURM_JOB_ID.$i.out"
    err="$directory/$SLURM_JOB_ID.$i.err"
    "${launcher[@]}" --container-image=` + framework.ShellQuote(enrootLifecycleImage) +
		" --container-workdir=/ --container-mounts=" + framework.ShellQuote(directory+":"+directory+":ro") + ` /bin/bash -eu "$directory/task.sh" > "$out" 2> "$err"
    rc=$?
    marker=0
    if ((rc == 0)); then
        token=$(sed -n 's/^TOKEN=//p' "$out")
        logfile=$(sed -n 's/^LOG_PATH=//p' "$out")
        if [[ -n "$token" && -n "$logfile" ]] && grep -Fxq -- "$token" "$logfile"; then marker=1; fi
    fi
    printf 'RESULT\t%d\t%d\t%d\n' "$i" "$rc" "$marker"
    if ((rc != 0 || marker != 1)); then cat "$out"; cat "$err" >&2; failures=$((failures+1)); fi
done
touch "$directory/$SLURM_JOB_ID.done"
test "$failures" -eq 0
`
}
