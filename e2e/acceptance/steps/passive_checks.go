package steps

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/soperator-e2e/acceptance/framework"
)

const (
	passiveCPUJobTimeout          = 3 * time.Minute
	passiveGPUJobTimeout          = 5 * time.Minute
	passiveAllocGPUDrainTimeout   = 3 * time.Minute
	passiveAllocGPUJobTimeout     = 4 * time.Minute
	passiveAllocGPURecoverTimeout = 3 * time.Minute
	passiveAllocMemDrainTimeout   = 3 * time.Minute
	passiveAllocMemJobTimeout     = 4 * time.Minute
	passiveAllocMemRecoverTimeout = 3 * time.Minute
	passiveTmpfsCleanupTimeout    = 1 * time.Minute

	passiveAllocGPUReason = "[user_problem] alloc_gpus_busy"
	passiveAllocMemReason = "[user_problem] alloc_mem_used"

	passiveAllocMemPressurePath = "/mnt/memory/soperator-acceptance-alloc-mem-used"
	allocMemPressureMargin      = uint64(2 * 1024 * 1024 * 1024)
	allocMemPressureMinimum     = uint64(1 * 1024 * 1024 * 1024)
	gpuCountCommand             = "set -o pipefail; timeout 30s nvidia-smi -L | wc -l"
)

var (
	keyValueLinePattern = regexp.MustCompile(`(?m)^([A-Z_]+)=(.+)$`)
)

type PassiveChecks struct {
	exec  framework.Exec
	slurm *framework.SlurmClient

	worker    framework.WorkerRef
	gpuWorker framework.WorkerRef

	cpuPrologOutput string
	cpuEpilogOutput string
	cpuHooksJob     framework.SbatchJob

	gpuHooksJob     framework.SbatchJob
	gpuHealthRunIDs []string

	tmpfsJobID   string
	tmpfsExisted bool

	allocGPUScenarioActive bool
	unmanagedGPUPID        string
	allocGPUJob            framework.SbatchJob
	gpuCount               int

	allocMemScenarioActive bool
	allocMemJob            framework.SbatchJob
}

func NewPassiveChecks(exec framework.Exec, slurm *framework.SlurmClient) *PassiveChecks {
	return &PassiveChecks{
		exec:  exec,
		slurm: slurm,
	}
}

func (s *PassiveChecks) Register(sc *godog.ScenarioContext) {
	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		s.cleanup(context.Background())
		return ctx, nil
	})

	sc.Step(`^a worker is selected$`, s.aWorkerIsSelected)
	sc.Step(`^a CPU-only Slurm job runs on the selected worker$`, s.aCPUOnlySlurmJobRunsOnTheSelectedWorker)
	sc.Step(`^the CPU job Prolog check runner output is fresh and healthy$`, s.theCPUJobPrologCheckRunnerOutputIsFreshAndHealthy)
	sc.Step(`^the CPU job Epilog check runner output is fresh and healthy$`, s.theCPUJobEpilogCheckRunnerOutputIsFreshAndHealthy)
	sc.Step(`^GPU-only passive checks are not executed for the CPU job$`, s.gpuOnlyPassiveChecksAreNotExecutedForTheCPUJob)
	sc.Step(`^the drop_page_cache passive check completed in Epilog$`, s.theDropPageCachePassiveCheckCompletedInEpilog)
	sc.Step(`^memory pressure is created on the selected worker$`, s.memoryPressureIsCreatedOnTheSelectedWorker)
	sc.Step(`^an all-memory Slurm job is submitted to the selected worker$`, s.anAllMemorySlurmJobIsSubmittedToTheSelectedWorker)
	sc.Step(`^the selected worker is drained by alloc_mem_used$`, s.theSelectedWorkerIsDrainedByAllocMemUsed)
	sc.Step(`^the memory pressure is removed$`, s.theMemoryPressureIsRemoved)
	sc.Step(`^HealthCheckProgram runs on the selected worker$`, s.healthCheckProgramRunsOnTheSelectedWorker)
	sc.Step(`^the selected worker no longer has alloc_mem_used reason$`, s.theSelectedWorkerNoLongerHasAllocMemUsedReason)
	sc.Step(`^the selected worker is usable after alloc_mem_used$`, s.theSelectedWorkerIsUsableAfterAllocMemUsed)
	sc.Step(`^a GPU worker is selected$`, s.aGPUWorkerIsSelected)
	sc.Step(`^a small GPU Slurm job runs on the selected GPU worker$`, s.aSmallGPUSlurmJobRunsOnTheSelectedGPUWorker)
	sc.Step(`^the GPU job health-check Prolog report is fresh and passing$`, s.theGPUJobHealthCheckPrologReportIsFreshAndPassing)
	sc.Step(`^the GPU job health-check Epilog report is fresh and passing$`, s.theGPUJobHealthCheckEpilogReportIsFreshAndPassing)
	sc.Step(`^raw GPU health-check command outputs are present$`, s.rawGPUHealthCheckCommandOutputsArePresent)
	sc.Step(`^a Slurm job checks its job tmpfs directory on the selected worker$`, s.aSlurmJobChecksItsJobTmpfsDirectoryOnTheSelectedWorker)
	sc.Step(`^the job tmpfs directory existed during the job$`, s.theJobTmpfsDirectoryExistedDuringTheJob)
	sc.Step(`^the job tmpfs directory is removed after the job exits$`, s.theJobTmpfsDirectoryIsRemovedAfterTheJobExits)
	sc.Step(`^an unmanaged GPU workload is started on the selected GPU worker$`, s.anUnmanagedGPUWorkloadIsStartedOnTheSelectedGPUWorker)
	sc.Step(`^a full-node GPU Slurm job is submitted to the selected GPU worker$`, s.aFullNodeGPUSlurmJobIsSubmittedToTheSelectedGPUWorker)
	sc.Step(`^the selected GPU worker is drained by alloc_gpus_busy$`, s.theSelectedGPUWorkerIsDrainedByAllocGPUsBusy)
	sc.Step(`^the unmanaged GPU workload is stopped$`, s.theUnmanagedGPUWorkloadIsStopped)
	sc.Step(`^the selected GPU worker no longer has alloc_gpus_busy reason$`, s.theSelectedGPUWorkerNoLongerHasAllocGPUsBusyReason)
	sc.Step(`^the selected GPU worker is usable after alloc_gpus_busy$`, s.theSelectedGPUWorkerIsUsableAfterAllocGPUsBusy)
}

func (s *PassiveChecks) aWorkerIsSelected(ctx context.Context) error {
	if s.worker.Name != "" {
		return nil
	}
	workers, err := s.slurm.AnyWorkers(1)
	if err != nil {
		return err
	}
	s.worker = workers[0]
	s.exec.Logf("passive checks: selected worker=%s", s.worker.Name)
	return nil
}

func (s *PassiveChecks) aCPUOnlySlurmJobRunsOnTheSelectedWorker(ctx context.Context) error {
	if err := s.ensureWorkerSelected(ctx); err != nil {
		return err
	}
	s.cpuHooksJob = framework.SbatchJob{}

	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-cpu-hooks",
		Nodes:        1,
		Nodelist:     []string{s.worker.Name},
		TasksPerNode: 1,
		Wrap:         "true",
	})
	if err != nil {
		return err
	}
	if err := waitForJobSucceeded(ctx, s.exec, s.slurm, job, passiveCPUJobTimeout); err != nil {
		return err
	}
	s.cpuHooksJob = job
	return nil
}

func (s *PassiveChecks) theCPUJobPrologCheckRunnerOutputIsFreshAndHealthy(ctx context.Context) error {
	output, err := s.cpuCheckRunnerOutput(ctx, "prolog")
	if err != nil {
		return err
	}
	s.cpuPrologOutput = output
	return assertLoggedCheckOutputsExist(ctx, s.exec, s.worker, output)
}

func (s *PassiveChecks) theCPUJobEpilogCheckRunnerOutputIsFreshAndHealthy(ctx context.Context) error {
	output, err := s.cpuCheckRunnerOutput(ctx, "epilog")
	if err != nil {
		return err
	}
	s.cpuEpilogOutput = output
	return assertLoggedCheckOutputsExist(ctx, s.exec, s.worker, output)
}

func (s *PassiveChecks) cpuCheckRunnerOutput(ctx context.Context, hook string) (string, error) {
	if s.cpuHooksJob.IsZero() {
		return "", fmt.Errorf("CPU hook job is not captured")
	}
	output, err := waitForHealthyCheckRunnerOutputForJob(ctx, s.exec, s.worker, checkRunnerOutputPath(s.worker.Name, hook), s.cpuHooksJob.ID, passiveCPUJobTimeout)
	if err != nil {
		return "", fmt.Errorf("%s check_runner output: %w", hook, err)
	}
	return output, nil
}

func (s *PassiveChecks) gpuOnlyPassiveChecksAreNotExecutedForTheCPUJob(ctx context.Context) error {
	if s.cpuPrologOutput == "" || s.cpuEpilogOutput == "" {
		return fmt.Errorf("CPU job check_runner outputs are not captured")
	}
	if err := assertCheckRunnerCheckAbsent(s.cpuPrologOutput, gpuHealthCheckName); err != nil {
		return fmt.Errorf("CPU prolog: %w", err)
	}
	if err := assertCheckRunnerCheckAbsent(s.cpuEpilogOutput, gpuHealthCheckName); err != nil {
		return fmt.Errorf("CPU epilog: %w", err)
	}
	return nil
}

func (s *PassiveChecks) theDropPageCachePassiveCheckCompletedInEpilog(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	if s.cpuHooksJob.IsZero() {
		return fmt.Errorf("CPU hook job is not captured")
	}
	epilog, err := waitForHealthyCheckRunnerOutputForJob(ctx, s.exec, s.worker, checkRunnerOutputPath(s.worker.Name, "epilog"), s.cpuHooksJob.ID, passiveCPUJobTimeout)
	if err != nil {
		return err
	}
	if err := assertCheckRunnerCheckOK(epilog, "drop_page_cache"); err != nil {
		return err
	}
	return assertLoggedCheckOutputsExist(ctx, s.exec, s.worker, epilog)
}

func (s *PassiveChecks) memoryPressureIsCreatedOnTheSelectedWorker(ctx context.Context) error {
	if err := s.ensureWorkerSelected(ctx); err != nil {
		return err
	}
	availableBytes, err := s.availableMemoryBytes(ctx, s.worker)
	if err != nil {
		return err
	}
	realBytes, err := s.realMemoryBytes(ctx, s.worker)
	if err != nil {
		return err
	}
	pressureBytes, err := allocMemPressureBytes(availableBytes, realBytes)
	if err != nil {
		return err
	}

	s.allocMemScenarioActive = true
	cmd := fmt.Sprintf("rm -f %s && fallocate --length %d %s && ls -lh %s",
		framework.ShellQuote(passiveAllocMemPressurePath),
		pressureBytes,
		framework.ShellQuote(passiveAllocMemPressurePath),
		framework.ShellQuote(passiveAllocMemPressurePath),
	)
	if _, err := s.exec.Worker(s.worker).RunWithDefaultRetry(ctx, cmd); err != nil {
		return fmt.Errorf("create memory pressure on %s: %w", s.worker.Name, err)
	}
	s.exec.Logf("alloc_mem_used: created %d bytes of memory pressure on %s", pressureBytes, s.worker.Name)
	return nil
}

func (s *PassiveChecks) anAllMemorySlurmJobIsSubmittedToTheSelectedWorker(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-alloc-mem-used",
		Nodes:        1,
		Nodelist:     []string{s.worker.Name},
		TasksPerNode: 1,
		ExtraFlags:   []string{"--mem=0", "--no-requeue"},
		Wrap:         "true",
	})
	if err != nil {
		return err
	}
	s.allocMemJob = job

	// alloc_mem_used should reject the allocation in Prolog while the node has
	// less available memory than the job requested, then drain the node.
	if err := framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, job, s.slurm.WaitForJobGone(ctx, job.ID, passiveAllocMemJobTimeout)); err != nil {
		return err
	}

	info, err := s.slurm.JobInfo(ctx, job.ID)
	if err != nil {
		return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, job, err)
	}
	if !info.SacctFound {
		return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, job,
			fmt.Errorf("expected Slurm job %s to be recorded in sacct after Prolog failure, got:\n%s", job.ID, info.SacctDump))
	}
	if info.CompletedSuccessfully() {
		return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, job,
			fmt.Errorf("expected Slurm job %s to fail in Prolog due to alloc_mem_used, got state=%s exit_code=%s", job.ID, info.SacctState, info.SacctExit))
	}
	if info.IsAlive() {
		return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, job,
			fmt.Errorf("expected Slurm job %s to be finished after leaving squeue, got state=%s exit_code=%s", job.ID, info.SacctState, info.SacctExit))
	}
	s.exec.Logf("alloc_mem_used trigger job %s finished as expected: state=%s exit_code=%s", job.ID, info.SacctState, info.SacctExit)
	s.allocMemJob = framework.SbatchJob{}
	return nil
}

func (s *PassiveChecks) theSelectedWorkerIsDrainedByAllocMemUsed(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	return s.slurm.WaitForNodeReasonContains(ctx, s.worker.Name, passiveAllocMemReason, passiveAllocMemDrainTimeout)
}

func (s *PassiveChecks) theMemoryPressureIsRemoved(ctx context.Context) error {
	return s.removeMemoryPressure(ctx)
}

func (s *PassiveChecks) healthCheckProgramRunsOnTheSelectedWorker(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	return runManualHCProgram(ctx, s.exec, s.worker)
}

func (s *PassiveChecks) theSelectedWorkerNoLongerHasAllocMemUsedReason(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	return s.slurm.WaitForNodeReasonCleared(ctx, s.worker.Name, passiveAllocMemReason, passiveAllocMemRecoverTimeout)
}

func (s *PassiveChecks) theSelectedWorkerIsUsableAfterAllocMemUsed(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	if err := s.slurm.WaitForNodeUsable(ctx, s.worker.Name, passiveAllocMemRecoverTimeout); err != nil {
		return err
	}
	s.allocMemScenarioActive = false
	return nil
}

func (s *PassiveChecks) aGPUWorkerIsSelected(ctx context.Context) error {
	if s.gpuWorker.Name != "" {
		return nil
	}
	workers, err := s.slurm.AnyGPUWorkers(1)
	if err != nil {
		return err
	}
	s.gpuWorker = workers[0]
	s.worker = s.gpuWorker
	s.exec.Logf("passive checks: selected GPU worker=%s", s.gpuWorker.Name)
	return nil
}

func (s *PassiveChecks) aSmallGPUSlurmJobRunsOnTheSelectedGPUWorker(ctx context.Context) error {
	if err := s.ensureGPUWorkerSelected(ctx); err != nil {
		return err
	}
	s.gpuHooksJob = framework.SbatchJob{}

	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-gpu-hooks",
		Nodes:        1,
		Nodelist:     []string{s.gpuWorker.Name},
		GPUsPerNode:  1,
		TasksPerNode: 1,
		Wrap:         "true",
	})
	if err != nil {
		return err
	}
	if err := waitForJobSucceeded(ctx, s.exec, s.slurm, job, passiveGPUJobTimeout); err != nil {
		return err
	}
	s.gpuHooksJob = job
	return nil
}

func (s *PassiveChecks) theGPUJobHealthCheckPrologReportIsFreshAndPassing(ctx context.Context) error {
	return s.gpuJobHealthCheckReportIsFreshAndPassing(ctx, "prolog")
}

func (s *PassiveChecks) theGPUJobHealthCheckEpilogReportIsFreshAndPassing(ctx context.Context) error {
	return s.gpuJobHealthCheckReportIsFreshAndPassing(ctx, "epilog")
}

func (s *PassiveChecks) gpuJobHealthCheckReportIsFreshAndPassing(ctx context.Context, hook string) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	if s.gpuHooksJob.IsZero() {
		return fmt.Errorf("GPU hook job is not captured")
	}
	ids, err := waitForPassingHealthCheckReportsForJob(ctx, s.exec, s.gpuWorker, gpuHealthCheckOutputPath(s.gpuWorker.Name, hook), s.gpuHooksJob.ID, passiveGPUJobTimeout)
	if err != nil {
		return fmt.Errorf("%s %s report: %w", hook, gpuHealthCheckName, err)
	}
	s.gpuHealthRunIDs = append(s.gpuHealthRunIDs, ids...)
	return nil
}

func (s *PassiveChecks) rawGPUHealthCheckCommandOutputsArePresent(ctx context.Context) error {
	if len(s.gpuHealthRunIDs) == 0 {
		return fmt.Errorf("GPU health-check run IDs are not captured")
	}
	return assertHealthCheckRawOutputsPresent(ctx, s.exec, s.gpuWorker, s.gpuHealthRunIDs)
}

func (s *PassiveChecks) aSlurmJobChecksItsJobTmpfsDirectoryOnTheSelectedWorker(ctx context.Context) error {
	if err := s.ensureWorkerSelected(ctx); err != nil {
		return err
	}
	wrap := `printf 'JOB_ID=%s\n' "$SLURM_JOB_ID"; test -d "/mnt/memory/job_${SLURM_JOB_ID}"`
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-job-tmpfs",
		Nodes:        1,
		Nodelist:     []string{s.worker.Name},
		TasksPerNode: 1,
		Wrap:         wrap,
	})
	if err != nil {
		return err
	}
	if err := waitForJobSucceeded(ctx, s.exec, s.slurm, job, passiveCPUJobTimeout); err != nil {
		return err
	}
	stdout, err := readJobFile(ctx, s.exec, job.StdoutPath)
	if err != nil {
		return err
	}
	jobID := parseKeyValueLine(stdout, "JOB_ID")
	if jobID == "" {
		return fmt.Errorf("job tmpfs job id not found in stdout:\n%s", strings.TrimSpace(stdout))
	}
	s.tmpfsJobID = jobID
	s.tmpfsExisted = true
	return nil
}

func (s *PassiveChecks) theJobTmpfsDirectoryExistedDuringTheJob(ctx context.Context) error {
	if !s.tmpfsExisted || s.tmpfsJobID == "" {
		return fmt.Errorf("job tmpfs directory existence was not confirmed during the job")
	}
	return nil
}

func (s *PassiveChecks) theJobTmpfsDirectoryIsRemovedAfterTheJobExits(ctx context.Context) error {
	if s.worker.Name == "" || s.tmpfsJobID == "" {
		return fmt.Errorf("job tmpfs worker or job id is not captured")
	}
	targets := []string{
		"/mnt/memory/job_" + s.tmpfsJobID,
		"/mnt/jail/mnt/memory/job_" + s.tmpfsJobID,
	}
	if err := s.exec.WaitFor(ctx, "job tmpfs directory removed", passiveTmpfsCleanupTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		var checks []string
		for _, target := range targets {
			checks = append(checks, fmt.Sprintf("test ! -e %s", framework.ShellQuote(target)))
		}
		probe := fmt.Sprintf("if %s; then echo gone; else echo exists; fi", strings.Join(checks, " && "))
		out, err := s.exec.Worker(s.worker).Run(waitCtx, probe)
		if err != nil {
			return false, fmt.Errorf("check job tmpfs directory removal: %w", err)
		}
		state := strings.TrimSpace(out)
		if state == "gone" {
			return true, nil
		}
		if state == "exists" {
			return false, nil
		}
		return false, fmt.Errorf("unexpected job tmpfs removal probe output: %q", state)
	}); err != nil {
		return err
	}
	s.tmpfsJobID = ""
	s.tmpfsExisted = false
	return nil
}

func (s *PassiveChecks) anUnmanagedGPUWorkloadIsStartedOnTheSelectedGPUWorker(ctx context.Context) error {
	if err := s.ensureGPUWorkerSelected(ctx); err != nil {
		return err
	}
	s.allocGPUScenarioActive = true
	script := "CUDA_VISIBLE_DEVICES=0 nohup all_reduce_perf -b 1 -e 1 -g 1 -N 0 >/dev/null 2>&1 & echo $!"

	out, err := s.exec.Worker(s.gpuWorker).RunWithDefaultRetry(ctx, framework.BashLC(script))
	if err != nil {
		return fmt.Errorf("start unmanaged GPU workload on %s: %w", s.gpuWorker.Name, err)
	}
	pid := strings.TrimSpace(out)
	if _, err := strconv.Atoi(pid); err != nil {
		return fmt.Errorf("parse unmanaged GPU workload PID from %q: %w", strings.TrimSpace(out), err)
	}
	s.unmanagedGPUPID = pid
	return s.waitForGPUComputeProcess(ctx)
}

func (s *PassiveChecks) aFullNodeGPUSlurmJobIsSubmittedToTheSelectedGPUWorker(ctx context.Context) error {
	if err := s.ensureGPUWorkerSelected(ctx); err != nil {
		return err
	}
	if err := s.ensureGPUCountDiscovered(ctx); err != nil {
		return err
	}

	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-alloc-gpus-busy",
		Nodes:        1,
		Nodelist:     []string{s.gpuWorker.Name},
		GPUsPerNode:  s.gpuCount,
		TasksPerNode: 1,
		ExtraFlags:   []string{"--no-requeue"},
		Wrap:         "true",
	})
	if err != nil {
		return err
	}
	s.allocGPUJob = job

	// This job is not expected to run successfully. It requests every GPU on
	// the worker while one GPU is already occupied by an unmanaged process, so
	// alloc_gpus_busy should reject the allocation in Prolog and drain the node.
	//
	// Prolog failure still runs Slurm/Epilog cleanup and can keep the job in
	// COMPLETING for a few minutes. Wait for squeue to clear so later scenarios
	// do not inherit unfinished cleanup from this disruptive check.
	if err := framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, job, s.slurm.WaitForJobGone(ctx, job.ID, passiveAllocGPUJobTimeout)); err != nil {
		return err
	}

	// After the job leaves squeue, verify sacct recorded a terminal non-success
	// outcome. COMPLETED/0:0 would mean the Prolog guard did not catch the
	// unmanaged GPU process.
	info, err := s.slurm.JobInfo(ctx, job.ID)
	if err != nil {
		return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, job, err)
	}
	if !info.SacctFound {
		return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, job,
			fmt.Errorf("expected Slurm job %s to be recorded in sacct after Prolog failure, got:\n%s", job.ID, info.SacctDump))
	}
	if info.CompletedSuccessfully() {
		return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, job,
			fmt.Errorf("expected Slurm job %s to fail in Prolog due to alloc_gpus_busy, got state=%s exit_code=%s", job.ID, info.SacctState, info.SacctExit))
	}
	if info.IsAlive() {
		return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, job,
			fmt.Errorf("expected Slurm job %s to be finished after leaving squeue, got state=%s exit_code=%s", job.ID, info.SacctState, info.SacctExit))
	}
	s.exec.Logf("alloc_gpus_busy trigger job %s finished as expected: state=%s exit_code=%s", job.ID, info.SacctState, info.SacctExit)
	s.allocGPUJob = framework.SbatchJob{}
	return nil
}

func (s *PassiveChecks) theSelectedGPUWorkerIsDrainedByAllocGPUsBusy(ctx context.Context) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	return s.slurm.WaitForNodeReasonContains(ctx, s.gpuWorker.Name, passiveAllocGPUReason, passiveAllocGPUDrainTimeout)
}

func (s *PassiveChecks) theUnmanagedGPUWorkloadIsStopped(ctx context.Context) error {
	return s.stopUnmanagedGPUWorkload(ctx)
}

func (s *PassiveChecks) theSelectedGPUWorkerNoLongerHasAllocGPUsBusyReason(ctx context.Context) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	return s.slurm.WaitForNodeReasonCleared(ctx, s.gpuWorker.Name, passiveAllocGPUReason, passiveAllocGPURecoverTimeout)
}

func (s *PassiveChecks) theSelectedGPUWorkerIsUsableAfterAllocGPUsBusy(ctx context.Context) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	if err := s.slurm.WaitForNodeUsable(ctx, s.gpuWorker.Name, passiveAllocGPURecoverTimeout); err != nil {
		return err
	}
	s.allocGPUScenarioActive = false
	return nil
}

func (s *PassiveChecks) cleanup(ctx context.Context) {
	s.cleanupAllocMem(ctx)
	s.cleanupAllocGPU(ctx)
	s.cleanupTmpfs(ctx)
}

func (s *PassiveChecks) cleanupAllocMem(ctx context.Context) {
	if s.allocMemScenarioActive {
		if !s.allocMemJob.IsZero() {
			if err := s.slurm.CancelJob(ctx, s.allocMemJob.ID, passiveAllocMemJobTimeout); err != nil {
				s.exec.Logf("cleanup: cancel alloc_mem_used trigger job %s: %v", s.allocMemJob.ID, err)
			}
			s.allocMemJob = framework.SbatchJob{}
		}
		if err := s.removeMemoryPressure(ctx); err != nil {
			s.exec.Logf("cleanup: remove alloc_mem_used memory pressure: %v", err)
		}
		if s.worker.Name != "" {
			if err := runManualHCProgram(ctx, s.exec, s.worker); err != nil {
				s.exec.Logf("cleanup: run hc_program on %s: %v", s.worker.Name, err)
			}
			if err := s.slurm.ResumeNodeIfDrainedByReason(ctx, s.worker.Name, passiveAllocMemReason); err != nil {
				s.exec.Logf("cleanup: resume %s after alloc_mem_used: %v", s.worker.Name, err)
			}
		}
		s.allocMemScenarioActive = false
	}
}

func (s *PassiveChecks) cleanupAllocGPU(ctx context.Context) {
	if s.allocGPUScenarioActive {
		if !s.allocGPUJob.IsZero() {
			if err := s.slurm.CancelJob(ctx, s.allocGPUJob.ID, passiveAllocGPUJobTimeout); err != nil {
				s.exec.Logf("cleanup: cancel alloc_gpus_busy trigger job %s: %v", s.allocGPUJob.ID, err)
			}
			s.allocGPUJob = framework.SbatchJob{}
		}
		if err := s.stopUnmanagedGPUWorkload(ctx); err != nil {
			s.exec.Logf("cleanup: stop unmanaged GPU workload: %v", err)
		}
		if s.gpuWorker.Name != "" {
			if err := runManualHCProgram(ctx, s.exec, s.gpuWorker); err != nil {
				s.exec.Logf("cleanup: run hc_program on %s: %v", s.gpuWorker.Name, err)
			}
			if err := s.slurm.ResumeNodeIfDrainedByReason(ctx, s.gpuWorker.Name, passiveAllocGPUReason); err != nil {
				s.exec.Logf("cleanup: resume %s after alloc_gpus_busy: %v", s.gpuWorker.Name, err)
			}
		}
		s.allocGPUScenarioActive = false
	}
}

func (s *PassiveChecks) cleanupTmpfs(ctx context.Context) {
	if s.worker.Name != "" && s.tmpfsJobID != "" {
		for _, target := range []string{
			"/mnt/memory/job_" + s.tmpfsJobID,
			"/mnt/jail/mnt/memory/job_" + s.tmpfsJobID,
		} {
			if _, err := s.exec.Worker(s.worker).Run(ctx, fmt.Sprintf("rm -rf %s >/dev/null 2>&1 || true", framework.ShellQuote(target))); err != nil {
				s.exec.Logf("cleanup: remove job tmpfs %s on %s: %v", target, s.worker.Name, err)
			}
		}
		s.tmpfsJobID = ""
		s.tmpfsExisted = false
	}
}

func (s *PassiveChecks) ensureWorkerSelected(ctx context.Context) error {
	if s.worker.Name != "" {
		return nil
	}
	return s.aWorkerIsSelected(ctx)
}

func (s *PassiveChecks) ensureGPUWorkerSelected(ctx context.Context) error {
	if s.gpuWorker.Name != "" {
		return nil
	}
	return s.aGPUWorkerIsSelected(ctx)
}

func (s *PassiveChecks) ensureGPUCountDiscovered(ctx context.Context) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	if s.gpuCount > 0 {
		return nil
	}
	out, err := s.exec.Worker(s.gpuWorker).RunWithDefaultRetry(ctx, framework.BashLC(gpuCountCommand))
	if err != nil {
		return fmt.Errorf("count GPUs on %s: %w", s.gpuWorker.Name, err)
	}
	count, err := parseGPUCount(s.gpuWorker.Name, out)
	if err != nil {
		return err
	}
	s.gpuCount = count
	s.exec.Logf("passive checks: discovered GPU count worker=%s gpu_count=%d", s.gpuWorker.Name, s.gpuCount)
	return nil
}

func parseGPUCount(workerName, output string) (int, error) {
	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil || count <= 0 {
		return 0, fmt.Errorf("invalid GPU count on %s: %q", workerName, strings.TrimSpace(output))
	}
	return count, nil
}

func (s *PassiveChecks) availableMemoryBytes(ctx context.Context, worker framework.WorkerRef) (uint64, error) {
	out, err := s.exec.Worker(worker).RunWithDefaultRetry(ctx, "free -b | awk '/^Mem:/ {print $7}'")
	if err != nil {
		return 0, fmt.Errorf("read available memory on %s: %w", worker.Name, err)
	}
	value, err := strconv.ParseUint(strings.TrimSpace(out), 10, 64)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("invalid available memory on %s: %q", worker.Name, strings.TrimSpace(out))
	}
	return value, nil
}

func (s *PassiveChecks) realMemoryBytes(ctx context.Context, worker framework.WorkerRef) (uint64, error) {
	node, err := s.slurm.NodeInfo(ctx, worker.Name)
	if err != nil {
		return 0, err
	}
	if node.RealMemoryMiB == 0 {
		return 0, fmt.Errorf("RealMemory was not found in Slurm node %s", worker.Name)
	}
	return node.RealMemoryMiB * 1024 * 1024, nil
}

func allocMemPressureBytes(availableBytes, realMemoryBytes uint64) (uint64, error) {
	if availableBytes == 0 || realMemoryBytes == 0 {
		return 0, fmt.Errorf("available memory and RealMemory must be non-zero")
	}
	targetAvailable := realMemoryBytes / 2
	if realMemoryBytes > allocMemPressureMargin {
		targetAvailable = realMemoryBytes - allocMemPressureMargin
	}

	var pressureBytes uint64
	if availableBytes > targetAvailable {
		pressureBytes = availableBytes - targetAvailable
	}
	if pressureBytes < allocMemPressureMinimum {
		pressureBytes = allocMemPressureMinimum
	}

	maxPressure := availableBytes / 2
	if pressureBytes > maxPressure {
		return 0, fmt.Errorf("memory pressure would require %d bytes with %d bytes available; refusing to consume more than half of available memory",
			pressureBytes, availableBytes)
	}
	return pressureBytes, nil
}

func (s *PassiveChecks) removeMemoryPressure(ctx context.Context) error {
	if s.worker.Name == "" {
		return nil
	}
	if _, err := s.exec.Worker(s.worker).Run(ctx, fmt.Sprintf("rm -f %s", framework.ShellQuote(passiveAllocMemPressurePath))); err != nil {
		return fmt.Errorf("remove memory pressure on %s: %w", s.worker.Name, err)
	}
	return nil
}

func (s *PassiveChecks) waitForGPUComputeProcess(ctx context.Context) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	if s.unmanagedGPUPID == "" {
		return fmt.Errorf("unmanaged GPU workload PID is not captured")
	}
	command := fmt.Sprintf("nvidia-smi -i 0 --query-compute-apps=pid --format=csv,noheader 2>/dev/null | awk 'NF' | grep -Fx %s >/dev/null && echo found || true",
		framework.ShellQuote(s.unmanagedGPUPID))
	return s.exec.WaitFor(ctx, "unmanaged GPU compute process visible", 2*time.Minute, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		out, err := s.exec.Worker(s.gpuWorker).RunWithDefaultRetry(waitCtx, command)
		if err != nil {
			return false, err
		}
		return strings.TrimSpace(out) == "found", nil
	})
}

func (s *PassiveChecks) stopUnmanagedGPUWorkload(ctx context.Context) error {
	if !s.allocGPUScenarioActive {
		return nil
	}
	worker := s.gpuWorker
	if worker.Name == "" {
		return nil
	}
	pid := strings.TrimSpace(s.unmanagedGPUPID)
	if pid == "" {
		return nil
	}
	command := fmt.Sprintf("kill %s >/dev/null 2>&1 || true; sleep 2; kill -9 %s >/dev/null 2>&1 || true",
		framework.ShellQuote(pid),
		framework.ShellQuote(pid),
	)
	_, err := s.exec.Worker(worker).Run(ctx, command)
	if err != nil {
		return fmt.Errorf("stop unmanaged GPU workload on %s: %w", worker.Name, err)
	}
	s.unmanagedGPUPID = ""
	return nil
}

func parseKeyValueLine(output, key string) string {
	for _, match := range keyValueLinePattern.FindAllStringSubmatch(output, -1) {
		if len(match) == 3 && match[1] == key {
			return strings.TrimSpace(match[2])
		}
	}
	return ""
}
