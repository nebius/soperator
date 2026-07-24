package steps

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	passiveCPUJobTimeout          = 5 * time.Minute
	passiveGPUJobTimeout          = 15 * time.Minute
	passiveAllocGPUDrainTimeout   = 5 * time.Minute
	passiveAllocGPUJobTimeout     = 12 * time.Minute
	passiveAllocGPURecoverTimeout = 7 * time.Minute
	passiveAllocMemDrainTimeout   = 5 * time.Minute
	passiveAllocMemJobTimeout     = 12 * time.Minute
	passiveAllocMemRecoverTimeout = 7 * time.Minute
	passiveTmpfsCleanupTimeout    = 2 * time.Minute

	passiveAllocGPUReason = "[user_problem] alloc_gpus_busy"
	passiveAllocMemReason = "[user_problem] alloc_mem_used"

	passiveAllocMemPressurePath = "/mnt/memory/soperator-acceptance-alloc-mem-used"
	allocMemPressureMargin      = uint64(2 * 1024 * 1024 * 1024)
	allocMemPressureMinimum     = uint64(1 * 1024 * 1024 * 1024)
)

var (
	keyValueLinePattern = regexp.MustCompile(`(?m)^([A-Z_]+)=(.+)$`)
	realMemoryPattern   = regexp.MustCompile(`\bRealMemory=(\d+)`)
)

type PassiveChecks struct {
	exec  framework.Exec
	slurm *framework.SlurmClient

	worker    string
	gpuWorker string
	gpuCount  int

	cpuPrologOutput string
	cpuEpilogOutput string

	gpuHealthRunIDs []string

	tmpfsJobID   string
	tmpfsExisted bool

	allocGPUScenarioActive bool
	unmanagedGPUPID        string
	allocGPUJob            framework.SbatchJob

	allocMemScenarioActive bool
	allocMemPressureBytes  uint64
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

	sc.Step(`^a worker is selected for passive checks$`, s.aWorkerIsSelectedForPassiveChecks)
	sc.Step(`^a CPU-only Slurm job runs on the selected worker$`, s.aCPUOnlySlurmJobRunsOnTheSelectedWorker)
	sc.Step(`^the CPU job Prolog and Epilog check runner outputs are fresh and healthy$`, s.theCPUJobPrologAndEpilogCheckRunnerOutputsAreFreshAndHealthy)
	sc.Step(`^GPU-only passive checks are not executed for the CPU job$`, s.gpuOnlyPassiveChecksAreNotExecutedForTheCPUJob)
	sc.Step(`^the drop_page_cache passive check completed in Epilog$`, s.theDropPageCachePassiveCheckCompletedInEpilog)
	sc.Step(`^memory pressure is created on the selected worker$`, s.memoryPressureIsCreatedOnTheSelectedWorker)
	sc.Step(`^an all-memory Slurm job is submitted to the selected worker$`, s.anAllMemorySlurmJobIsSubmittedToTheSelectedWorker)
	sc.Step(`^the selected worker is drained by alloc_mem_used$`, s.theSelectedWorkerIsDrainedByAllocMemUsed)
	sc.Step(`^the memory pressure is removed$`, s.theMemoryPressureIsRemoved)
	sc.Step(`^the selected worker recovers from alloc_mem_used$`, s.theSelectedWorkerRecoversFromAllocMemUsed)
	sc.Step(`^a GPU worker is selected for passive checks$`, s.aGPUWorkerIsSelectedForPassiveChecks)
	sc.Step(`^a small GPU Slurm job runs on the selected GPU worker$`, s.aSmallGPUSlurmJobRunsOnTheSelectedGPUWorker)
	sc.Step(`^the GPU job health-check Prolog and Epilog reports are fresh and passing$`, s.theGPUJobHealthCheckPrologAndEpilogReportsAreFreshAndPassing)
	sc.Step(`^raw GPU health-check command outputs are present$`, s.rawGPUHealthCheckCommandOutputsArePresent)
	sc.Step(`^a Slurm job checks its job tmpfs directory on the selected worker$`, s.aSlurmJobChecksItsJobTmpfsDirectoryOnTheSelectedWorker)
	sc.Step(`^the job tmpfs directory existed during the job$`, s.theJobTmpfsDirectoryExistedDuringTheJob)
	sc.Step(`^the job tmpfs directory is removed after the job exits$`, s.theJobTmpfsDirectoryIsRemovedAfterTheJobExits)
	sc.Step(`^an unmanaged GPU workload is started on the selected GPU worker$`, s.anUnmanagedGPUWorkloadIsStartedOnTheSelectedGPUWorker)
	sc.Step(`^a full-node GPU Slurm job is submitted to the selected GPU worker$`, s.aFullNodeGPUSlurmJobIsSubmittedToTheSelectedGPUWorker)
	sc.Step(`^the selected GPU worker is drained by alloc_gpus_busy$`, s.theSelectedGPUWorkerIsDrainedByAllocGPUsBusy)
	sc.Step(`^the unmanaged GPU workload is stopped$`, s.theUnmanagedGPUWorkloadIsStopped)
	sc.Step(`^the selected GPU worker recovers from alloc_gpus_busy$`, s.theSelectedGPUWorkerRecoversFromAllocGPUsBusy)
}

func (s *PassiveChecks) aWorkerIsSelectedForPassiveChecks(ctx context.Context) error {
	if s.worker != "" {
		return nil
	}
	workers, err := s.slurm.AnyWorkers(1)
	if err != nil {
		return err
	}
	s.worker = workers[0]
	s.exec.Logf("passive checks: selected worker=%s", s.worker)
	return nil
}

func (s *PassiveChecks) aCPUOnlySlurmJobRunsOnTheSelectedWorker(ctx context.Context) error {
	if err := s.ensureWorkerSelected(ctx); err != nil {
		return err
	}
	if err := removeJailFiles(ctx, s.exec,
		checkRunnerOutputPath(s.worker, "prolog"),
		checkRunnerOutputPath(s.worker, "epilog"),
	); err != nil {
		return err
	}

	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-cpu-hooks",
		Nodes:        1,
		Nodelist:     []string{s.worker},
		TasksPerNode: 1,
		Wrap:         "true",
	})
	if err != nil {
		return err
	}
	return waitForJobSucceeded(ctx, s.exec, s.slurm, job, passiveCPUJobTimeout)
}

func (s *PassiveChecks) theCPUJobPrologAndEpilogCheckRunnerOutputsAreFreshAndHealthy(ctx context.Context) error {
	prolog, err := waitForHealthyCheckRunnerOutput(ctx, s.exec, checkRunnerOutputPath(s.worker, "prolog"), passiveCPUJobTimeout)
	if err != nil {
		return err
	}
	epilog, err := waitForHealthyCheckRunnerOutput(ctx, s.exec, checkRunnerOutputPath(s.worker, "epilog"), passiveCPUJobTimeout)
	if err != nil {
		return err
	}
	for label, output := range map[string]string{"prolog": prolog, "epilog": epilog} {
		if err := assertLoggedCheckOutputsExist(ctx, s.exec, output); err != nil {
			return fmt.Errorf("%s check outputs: %w", label, err)
		}
	}
	s.cpuPrologOutput = prolog
	s.cpuEpilogOutput = epilog
	return nil
}

func (s *PassiveChecks) gpuOnlyPassiveChecksAreNotExecutedForTheCPUJob(ctx context.Context) error {
	if s.cpuPrologOutput == "" || s.cpuEpilogOutput == "" {
		return fmt.Errorf("CPU job check_runner outputs are not captured")
	}
	if err := assertCheckRunnerCheckAbsent(s.cpuPrologOutput, "gpu_health_check"); err != nil {
		return fmt.Errorf("CPU prolog: %w", err)
	}
	if err := assertCheckRunnerCheckAbsent(s.cpuEpilogOutput, "gpu_health_check"); err != nil {
		return fmt.Errorf("CPU epilog: %w", err)
	}
	return nil
}

func (s *PassiveChecks) theDropPageCachePassiveCheckCompletedInEpilog(ctx context.Context) error {
	if s.worker == "" {
		return fmt.Errorf("worker is not selected")
	}
	epilog, err := waitForHealthyCheckRunnerOutput(ctx, s.exec, checkRunnerOutputPath(s.worker, "epilog"), passiveCPUJobTimeout)
	if err != nil {
		return err
	}
	if err := assertCheckRunnerCheckOK(epilog, "drop_page_cache"); err != nil {
		return err
	}
	return assertLoggedCheckOutputsExist(ctx, s.exec, epilog)
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
	s.allocMemPressureBytes = pressureBytes
	cmd := fmt.Sprintf("rm -f %s && fallocate --length %d %s && ls -lh %s",
		framework.ShellQuote(passiveAllocMemPressurePath),
		pressureBytes,
		framework.ShellQuote(passiveAllocMemPressurePath),
		framework.ShellQuote(passiveAllocMemPressurePath),
	)
	if _, err := s.exec.Worker(s.worker).RunWithDefaultRetry(ctx, cmd); err != nil {
		return fmt.Errorf("create memory pressure on %s: %w", s.worker, err)
	}
	s.exec.Logf("alloc_mem_used: created %d bytes of memory pressure on %s", pressureBytes, s.worker)
	return nil
}

func (s *PassiveChecks) anAllMemorySlurmJobIsSubmittedToTheSelectedWorker(ctx context.Context) error {
	if s.worker == "" {
		return fmt.Errorf("worker is not selected")
	}
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-alloc-mem-used",
		Nodes:        1,
		Nodelist:     []string{s.worker},
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
	if s.worker == "" {
		return fmt.Errorf("worker is not selected")
	}
	return s.slurm.WaitForNodeReasonContains(ctx, s.worker, passiveAllocMemReason, passiveAllocMemDrainTimeout)
}

func (s *PassiveChecks) theMemoryPressureIsRemoved(ctx context.Context) error {
	return s.removeMemoryPressure(ctx)
}

func (s *PassiveChecks) theSelectedWorkerRecoversFromAllocMemUsed(ctx context.Context) error {
	if s.worker == "" {
		return fmt.Errorf("worker is not selected")
	}
	if err := runManualHCProgram(ctx, s.exec, s.worker); err != nil {
		return err
	}
	if err := s.slurm.WaitForNodeUsableWithoutReason(ctx, s.worker, passiveAllocMemReason, passiveAllocMemRecoverTimeout); err != nil {
		return err
	}
	s.allocMemScenarioActive = false
	s.allocMemPressureBytes = 0
	return nil
}

func (s *PassiveChecks) aGPUWorkerIsSelectedForPassiveChecks(ctx context.Context) error {
	if s.gpuWorker != "" {
		return nil
	}
	workers, err := s.slurm.AnyGPUWorkers(1)
	if err != nil {
		return err
	}
	s.gpuWorker = workers[0]
	s.worker = s.gpuWorker

	count, err := s.gpuCountOnWorker(ctx, s.gpuWorker)
	if err != nil {
		return err
	}
	s.gpuCount = count
	s.exec.Logf("passive checks: selected GPU worker=%s gpu_count=%d", s.gpuWorker, s.gpuCount)
	return nil
}

func (s *PassiveChecks) aSmallGPUSlurmJobRunsOnTheSelectedGPUWorker(ctx context.Context) error {
	if err := s.ensureGPUWorkerSelected(ctx); err != nil {
		return err
	}
	if err := removeJailFiles(ctx, s.exec,
		gpuHealthCheckOutputPath(s.gpuWorker, "prolog"),
		gpuHealthCheckOutputPath(s.gpuWorker, "epilog"),
	); err != nil {
		return err
	}

	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-gpu-hooks",
		Nodes:        1,
		Nodelist:     []string{s.gpuWorker},
		GPUsPerNode:  1,
		TasksPerNode: 1,
		Wrap:         "true",
	})
	if err != nil {
		return err
	}
	return waitForJobSucceeded(ctx, s.exec, s.slurm, job, passiveGPUJobTimeout)
}

func (s *PassiveChecks) theGPUJobHealthCheckPrologAndEpilogReportsAreFreshAndPassing(ctx context.Context) error {
	if s.gpuWorker == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	var runIDs []string
	for _, hook := range []string{"prolog", "epilog"} {
		ids, err := waitForPassingHealthCheckReports(ctx, s.exec, gpuHealthCheckOutputPath(s.gpuWorker, hook), passiveGPUJobTimeout)
		if err != nil {
			return fmt.Errorf("%s gpu_health_check report: %w", hook, err)
		}
		runIDs = append(runIDs, ids...)
	}
	s.gpuHealthRunIDs = runIDs
	return nil
}

func (s *PassiveChecks) rawGPUHealthCheckCommandOutputsArePresent(ctx context.Context) error {
	if len(s.gpuHealthRunIDs) == 0 {
		return fmt.Errorf("GPU health-check run IDs are not captured")
	}
	return assertHealthCheckRawOutputsPresent(ctx, s.exec, s.gpuHealthRunIDs)
}

func (s *PassiveChecks) aSlurmJobChecksItsJobTmpfsDirectoryOnTheSelectedWorker(ctx context.Context) error {
	if err := s.ensureWorkerSelected(ctx); err != nil {
		return err
	}
	wrap := `printf 'JOB_ID=%s\n' "$SLURM_JOB_ID"; test -d "/mnt/memory/job_${SLURM_JOB_ID}"`
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-job-tmpfs",
		Nodes:        1,
		Nodelist:     []string{s.worker},
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
	if s.worker == "" || s.tmpfsJobID == "" {
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
		_, err := s.exec.Worker(s.worker).RunWithDefaultRetry(waitCtx, strings.Join(checks, " && "))
		if err != nil {
			return false, nil
		}
		return true, nil
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
		return fmt.Errorf("start unmanaged GPU workload on %s: %w", s.gpuWorker, err)
	}
	pid := strings.TrimSpace(out)
	if _, err := strconv.Atoi(pid); err != nil {
		return fmt.Errorf("parse unmanaged GPU workload PID from %q: %w", strings.TrimSpace(out), err)
	}
	s.unmanagedGPUPID = pid
	return s.waitForGPUComputeProcess(ctx)
}

func (s *PassiveChecks) aFullNodeGPUSlurmJobIsSubmittedToTheSelectedGPUWorker(ctx context.Context) error {
	if s.gpuWorker == "" || s.gpuCount == 0 {
		return fmt.Errorf("GPU worker or GPU count is not captured")
	}

	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-alloc-gpus-busy",
		Nodes:        1,
		Nodelist:     []string{s.gpuWorker},
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
	if s.gpuWorker == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	return s.slurm.WaitForNodeReasonContains(ctx, s.gpuWorker, passiveAllocGPUReason, passiveAllocGPUDrainTimeout)
}

func (s *PassiveChecks) theUnmanagedGPUWorkloadIsStopped(ctx context.Context) error {
	return s.stopUnmanagedGPUWorkload(ctx)
}

func (s *PassiveChecks) theSelectedGPUWorkerRecoversFromAllocGPUsBusy(ctx context.Context) error {
	if s.gpuWorker == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	if err := runManualHCProgram(ctx, s.exec, s.gpuWorker); err != nil {
		return err
	}
	if err := s.slurm.WaitForNodeUsableWithoutReason(ctx, s.gpuWorker, passiveAllocGPUReason, passiveAllocGPURecoverTimeout); err != nil {
		return err
	}
	s.allocGPUScenarioActive = false
	return nil
}

func (s *PassiveChecks) cleanup(ctx context.Context) {
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
		if s.worker != "" {
			if err := runManualHCProgram(ctx, s.exec, s.worker); err != nil {
				s.exec.Logf("cleanup: run hc_program on %s: %v", s.worker, err)
			}
			if err := s.slurm.ResumeNodeIfDrainedByReason(ctx, s.worker, passiveAllocMemReason); err != nil {
				s.exec.Logf("cleanup: resume %s after alloc_mem_used: %v", s.worker, err)
			}
		}
		s.allocMemScenarioActive = false
		s.allocMemPressureBytes = 0
	}
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
		if s.gpuWorker != "" {
			if err := runManualHCProgram(ctx, s.exec, s.gpuWorker); err != nil {
				s.exec.Logf("cleanup: run hc_program on %s: %v", s.gpuWorker, err)
			}
			if err := s.slurm.ResumeNodeIfDrainedByReason(ctx, s.gpuWorker, passiveAllocGPUReason); err != nil {
				s.exec.Logf("cleanup: resume %s after alloc_gpus_busy: %v", s.gpuWorker, err)
			}
		}
		s.allocGPUScenarioActive = false
	}
	if s.worker != "" && s.tmpfsJobID != "" {
		for _, target := range []string{
			"/mnt/memory/job_" + s.tmpfsJobID,
			"/mnt/jail/mnt/memory/job_" + s.tmpfsJobID,
		} {
			if _, err := s.exec.Worker(s.worker).Run(ctx, fmt.Sprintf("rm -rf %s >/dev/null 2>&1 || true", framework.ShellQuote(target))); err != nil {
				s.exec.Logf("cleanup: remove job tmpfs %s on %s: %v", target, s.worker, err)
			}
		}
		s.tmpfsJobID = ""
		s.tmpfsExisted = false
	}
}

func (s *PassiveChecks) ensureWorkerSelected(ctx context.Context) error {
	if s.worker != "" {
		return nil
	}
	return s.aWorkerIsSelectedForPassiveChecks(ctx)
}

func (s *PassiveChecks) ensureGPUWorkerSelected(ctx context.Context) error {
	if s.gpuWorker != "" {
		return nil
	}
	return s.aGPUWorkerIsSelectedForPassiveChecks(ctx)
}

func (s *PassiveChecks) gpuCountOnWorker(ctx context.Context, worker string) (int, error) {
	out, err := s.exec.Worker(worker).RunWithDefaultRetry(ctx, "nvidia-smi -L | wc -l")
	if err != nil {
		return 0, fmt.Errorf("count GPUs on %s: %w", worker, err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || count <= 0 {
		return 0, fmt.Errorf("invalid GPU count on %s: %q", worker, strings.TrimSpace(out))
	}
	return count, nil
}

func (s *PassiveChecks) availableMemoryBytes(ctx context.Context, worker string) (uint64, error) {
	out, err := s.exec.Worker(worker).RunWithDefaultRetry(ctx, "free -b | awk '/^Mem:/ {print $7}'")
	if err != nil {
		return 0, fmt.Errorf("read available memory on %s: %w", worker, err)
	}
	value, err := strconv.ParseUint(strings.TrimSpace(out), 10, 64)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("invalid available memory on %s: %q", worker, strings.TrimSpace(out))
	}
	return value, nil
}

func (s *PassiveChecks) realMemoryBytes(ctx context.Context, worker string) (uint64, error) {
	node, err := s.slurm.NodeInfo(ctx, worker)
	if err != nil {
		return 0, err
	}
	match := realMemoryPattern.FindStringSubmatch(node.Raw)
	if len(match) != 2 {
		return 0, fmt.Errorf("RealMemory was not found in Slurm node %s:\n%s", worker, node.Raw)
	}
	realMiB, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil || realMiB == 0 {
		return 0, fmt.Errorf("invalid RealMemory for Slurm node %s: %q", worker, match[1])
	}
	return realMiB * 1024 * 1024, nil
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
	if s.worker == "" {
		return nil
	}
	if _, err := s.exec.Worker(s.worker).Run(ctx, fmt.Sprintf("rm -f %s", framework.ShellQuote(passiveAllocMemPressurePath))); err != nil {
		return fmt.Errorf("remove memory pressure on %s: %w", s.worker, err)
	}
	s.allocMemPressureBytes = 0
	return nil
}

func (s *PassiveChecks) waitForGPUComputeProcess(ctx context.Context) error {
	if s.gpuWorker == "" {
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
	if worker == "" {
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
		return fmt.Errorf("stop unmanaged GPU workload on %s: %w", worker, err)
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
