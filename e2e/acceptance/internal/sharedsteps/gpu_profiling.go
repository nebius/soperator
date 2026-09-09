package sharedsteps

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	gpuProfilingUser = "soperatorchecks"
	// Profiling jobs run as soperatorchecks, so their logs and reports need a
	// user-writable directory. The E2E cluster teardown removes these artifacts.
	gpuProfilingOutputDir  = "/opt/soperator-home/soperatorchecks/.acceptance/gpu-profiling"
	gpuProfilingJobTimeout = 15 * time.Minute
	gpuProfilingCleanup    = 3 * time.Minute
)

type GPUProfiling struct {
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	selector *framework.WorkerSelector

	worker     framework.WorkerInfo
	job        framework.SbatchJob
	reportPath string
}

func NewGPUProfiling(runtime framework.Runtime, slurm *framework.SlurmClient, selector *framework.WorkerSelector) *GPUProfiling {
	return &GPUProfiling{
		runtime:  runtime,
		slurm:    slurm,
		selector: selector,
	}
}

func (s *GPUProfiling) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a GPU worker is available for profiling$`, s.aGPUWorkerIsAvailableForProfiling)
	sc.Step(`^the soperatorchecks user is available on the worker$`, s.theSoperatorchecksUserIsAvailableOnTheWorker)
	sc.Step(`^GPU profiling is enabled for non-root users on the worker$`, s.gpuProfilingIsEnabledForNonRootUsersOnTheWorker)
	sc.Step(`^the soperatorchecks user submits an Nsight Compute profiling job$`, s.theSoperatorchecksUserSubmitsAnNsightComputeProfilingJob)
	sc.Step(`^the Nsight Compute profiling job succeeds$`, s.theNsightComputeProfilingJobSucceeds)
	sc.Step(`^the soperatorchecks user submits a full-node Nsight Systems profiling job$`, s.theSoperatorchecksUserSubmitsAFullNodeNsightSystemsProfilingJob)
	sc.Step(`^the Nsight Systems profiling job succeeds and produces a report$`, s.theNsightSystemsProfilingJobSucceedsAndProducesAReport)
}

func (s *GPUProfiling) CleanupAndReset(ctx context.Context) {
	if !s.job.IsZero() {
		if err := s.slurm.CancelJob(ctx, s.job.ID, gpuProfilingCleanup); err != nil {
			s.runtime.Logf("cleanup: cancel GPU profiling job %s: %v", s.job.ID, err)
		}
	}

	s.worker = framework.WorkerInfo{}
	s.job = framework.SbatchJob{}
	s.reportPath = ""
}

func (s *GPUProfiling) aGPUWorkerIsAvailableForProfiling(ctx context.Context) error {
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	if s.worker.SlurmNode.GPUCount < 1 {
		return fmt.Errorf("GPU worker %s has invalid configured GPU count %d", s.worker.Name, s.worker.SlurmNode.GPUCount)
	}
	s.runtime.Logf("GPU profiling: selected worker=%s GPUs=%d", s.worker.Name, s.worker.SlurmNode.GPUCount)
	return nil
}

func (s *GPUProfiling) theSoperatorchecksUserIsAvailableOnTheWorker(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("GPU profiling worker is not selected")
	}
	if _, err := s.runtime.Jail().RunWithDefaultRetry(ctx,
		"id "+framework.ShellQuote(gpuProfilingUser)+" >/dev/null"); err != nil {
		return fmt.Errorf("check %s user in the jail: %w", gpuProfilingUser, err)
	}
	return waitForSSHTestUserOnWorker(ctx, s.runtime, gpuProfilingUser, s.worker)
}

func (s *GPUProfiling) gpuProfilingIsEnabledForNonRootUsersOnTheWorker(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("GPU profiling worker is not selected")
	}
	output, err := s.runtime.Worker(s.worker).RunWithDefaultRetry(ctx,
		"grep '^RmProfilingAdminOnly:' /proc/driver/nvidia/params")
	if err != nil {
		return fmt.Errorf("read NVIDIA profiling driver parameter on %s: %w", s.worker.Name, err)
	}
	fields := strings.Fields(output)
	if len(fields) != 2 || fields[0] != "RmProfilingAdminOnly:" || fields[1] != "0" {
		return fmt.Errorf("expected RmProfilingAdminOnly to be 0 on %s, got %q", s.worker.Name, strings.TrimSpace(output))
	}
	return nil
}

func (s *GPUProfiling) theSoperatorchecksUserSubmitsAnNsightComputeProfilingJob(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("GPU profiling worker is not selected")
	}
	wrap := fmt.Sprintf(
		"test \"$(id -u)\" -ne 0 && test \"$(id -un)\" = %s && export NCCL_GRAPH_DISABLE=1 && "+
			"ncu --target-processes all --profile-from-start on --replay-mode kernel --set base "+
			"all_reduce_perf -g 1 -b 32M -e 32M -f 1 -w 1 -n 1",
		framework.ShellQuote(gpuProfilingUser),
	)
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-gpu-ncu",
		Nodes:        1,
		Nodelist:     []string{s.worker.Name},
		GPUsPerNode:  1,
		TasksPerNode: 1,
		Wrap:         wrap,
		RunAsUser:    gpuProfilingUser,
		OutputDir:    gpuProfilingOutputDir,
	})
	if err != nil {
		return err
	}
	s.job = job
	s.runtime.Logf("Nsight Compute: worker=%s job_id=%s stdout=%s stderr=%s",
		s.worker.Name, job.ID, job.StdoutPath, job.StderrPath)
	return nil
}

func (s *GPUProfiling) theNsightComputeProfilingJobSucceeds(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("Nsight Compute job ID is empty")
	}
	return waitForJobSucceeded(ctx, s.runtime, s.slurm, s.job, gpuProfilingJobTimeout)
}

func (s *GPUProfiling) theSoperatorchecksUserSubmitsAFullNodeNsightSystemsProfilingJob(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("GPU profiling worker is not selected")
	}
	reportBase := path.Join(gpuProfilingOutputDir, fmt.Sprintf("nsys-%d", time.Now().UnixNano()))
	wrap := fmt.Sprintf(
		"test \"$(id -u)\" -ne 0 && test \"$(id -un)\" = %s && "+
			"nsys profile --trace=cuda,nvtx -o %s all_reduce_perf -b 512M -e 8G -f 2 -g %d",
		framework.ShellQuote(gpuProfilingUser),
		framework.ShellQuote(reportBase),
		s.worker.SlurmNode.GPUCount,
	)
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-gpu-nsys",
		Nodes:        1,
		Nodelist:     []string{s.worker.Name},
		GPUsPerNode:  s.worker.SlurmNode.GPUCount,
		TasksPerNode: 1,
		Wrap:         wrap,
		RunAsUser:    gpuProfilingUser,
		OutputDir:    gpuProfilingOutputDir,
	})
	if err != nil {
		return err
	}
	s.job = job
	s.reportPath = reportBase + ".nsys-rep"
	s.runtime.Logf("Nsight Systems: worker=%s GPUs=%d job_id=%s report=%s stdout=%s stderr=%s",
		s.worker.Name, s.worker.SlurmNode.GPUCount, job.ID, s.reportPath, job.StdoutPath, job.StderrPath)
	return nil
}

func (s *GPUProfiling) theNsightSystemsProfilingJobSucceedsAndProducesAReport(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("Nsight Systems job ID is empty")
	}
	if err := waitForJobSucceeded(ctx, s.runtime, s.slurm, s.job, gpuProfilingJobTimeout); err != nil {
		return err
	}
	if _, err := s.runtime.Worker(s.worker).RunWithDefaultRetry(ctx,
		"test -s "+framework.ShellQuote(s.reportPath)); err != nil {
		return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job,
			fmt.Errorf("expected non-empty Nsight Systems report %s on %s: %w", s.reportPath, s.worker.Name, err))
	}
	return nil
}
