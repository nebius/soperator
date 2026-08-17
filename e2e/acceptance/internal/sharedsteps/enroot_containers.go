package sharedsteps

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/soperator-e2e/acceptance/framework"
)

const (
	// Mirrored Ubuntu has /bin/bash required by the Slurm task prolog under Pyxis.
	enrootLifecycleImage   = "docker://cr.eu-north1.nebius.cloud#soperator/ubuntu:noble"
	enrootLifecycleCommand = "echo ready; sleep 3600"

	enrootSquashRoot = "/var/cache/enroot-container-images"

	enrootDedicatedDataPath = "/mnt/image-storage/enroot/data"

	enrootJobStartTimeout = 5 * time.Minute
	enrootProbeTimeout    = 5 * time.Minute
	enrootStopTimeout     = 3 * time.Minute
	enrootGPUSmokeTimeout = 10 * time.Minute
)

type EnrootContainers struct {
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	selector *framework.WorkerSelector

	workers          []framework.WorkerInfo
	connectionWorker framework.WorkerInfo
	job              framework.SbatchJob

	squashPath         string
	expectedSquashPath string
	squashStatBefore   string
	runtimeNamePrefix  string

	directSquashFS *bool
}

func NewEnrootContainers(runtime framework.Runtime, slurm *framework.SlurmClient, selector *framework.WorkerSelector) *EnrootContainers {
	return &EnrootContainers{
		runtime:  runtime,
		slurm:    slurm,
		selector: selector,
	}
}

func (s *EnrootContainers) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a long-running Enroot container job is submitted on two workers$`, s.aLongRunningEnrootContainerJobIsSubmittedOnTwoWorkers)
	sc.Step(`^the Enroot container job is running$`, s.theEnrootContainerJobIsRunning)
	sc.Step(`^an Enroot image artifact is present on a worker$`, s.anEnrootImageArtifactIsPresentOnAWorker)
	sc.Step(`^Enroot runtime state is visible while the job is running$`, s.enrootRuntimeStateIsVisibleWhileTheJobIsRunning)
	sc.Step(`^the Enroot container job is cancelled$`, s.theEnrootContainerJobIsCancelled)
	sc.Step(`^Enroot runtime state is cleaned up and the image artifact remains$`, s.enrootRuntimeStateIsCleanedUpAndTheImageArtifactRemains)
	sc.Step(`^the same Enroot container job is submitted again$`, s.theSameEnrootContainerJobIsSubmittedAgain)
	sc.Step(`^the existing Enroot image artifact is reused$`, s.theExistingEnrootImageArtifactIsReused)
	sc.Step(`^the repeated Enroot container job is cancelled$`, s.theRepeatedEnrootContainerJobIsCancelled)
	sc.Step(`^Enroot runtime state is cleaned up$`, s.enrootRuntimeStateIsCleanedUp)
	sc.Step(`^an Enroot GPU smoke job is submitted on one GPU worker$`, s.anEnrootGPUSmokeJobIsSubmittedOnOneGPUWorker)
	sc.Step(`^the Enroot GPU smoke job succeeds and reports visible GPUs$`, s.theEnrootGPUSmokeJobSucceedsAndReportsVisibleGPUs)
}

func (s *EnrootContainers) CleanupAndReset(ctx context.Context) {
	if cleanupErr := s.cancelCurrentJob(ctx); cleanupErr != nil {
		s.runtime.Logf("cleanup: cancel enroot job: %v", cleanupErr)
	}
	s.workers = nil
	s.connectionWorker = framework.WorkerInfo{}
	s.job = framework.SbatchJob{}
	s.squashPath = ""
	s.expectedSquashPath = ""
	s.squashStatBefore = ""
	s.runtimeNamePrefix = ""
	s.directSquashFS = nil
}

func (s *EnrootContainers) aLongRunningEnrootContainerJobIsSubmittedOnTwoWorkers(ctx context.Context) error {
	if len(s.workers) == 0 {
		workers, err := s.selector.PickWorkers(ctx, 2)
		if err != nil {
			return framework.SkipIfInsufficientWorkers(s.runtime, err)
		}
		s.workers = workers
		s.connectionWorker = workers[0]
		s.runtime.Logf("enroot containers: selected workers=%s", strings.Join(framework.WorkerNames(s.workers), ","))
	}
	s.squashPath = ""
	s.expectedSquashPath = ""
	s.squashStatBefore = ""
	s.runtimeNamePrefix = ""

	expectedSquashPath, err := s.expectedLifecycleSquashPath(ctx)
	if err != nil {
		return err
	}
	s.expectedSquashPath = expectedSquashPath

	return s.submitEnrootLifecycleJob(ctx, "e2e-enroot-initial")
}

func (s *EnrootContainers) theEnrootContainerJobIsRunning(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("enroot job id is empty")
	}
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job,
		s.slurm.WaitForJobRunning(ctx, s.job.ID, enrootJobStartTimeout))
}

func (s *EnrootContainers) anEnrootImageArtifactIsPresentOnAWorker(ctx context.Context) error {
	if s.connectionWorker.Name == "" {
		return fmt.Errorf("enroot connection worker is not selected")
	}

	err := framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, s.job, "enroot image artifact present",
		enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			found, err := s.expectedSquashArtifactExists(waitCtx)
			if err != nil {
				return false, err
			}
			if !found {
				return false, nil
			}
			s.squashPath = s.expectedSquashPath
			s.runtime.Logf("enroot containers: tracked squashfs path=%s", s.squashPath)
			return true, nil
		})
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job, err)
}

func (s *EnrootContainers) enrootRuntimeStateIsVisibleWhileTheJobIsRunning(ctx context.Context) error {
	directSquashFS, err := s.enrootDirectSquashFSEnabled(ctx)
	if err != nil {
		return err
	}
	if directSquashFS {
		return s.enrootSquashfsImageIsMountedDirectlyWhileTheJobIsRunning(ctx)
	}
	return s.enrootRuntimeContainerDataIsVisibleWhileTheJobIsRunning(ctx)
}

func (s *EnrootContainers) theEnrootContainerJobIsCancelled(ctx context.Context) error {
	return s.cancelCurrentJob(ctx)
}

func (s *EnrootContainers) enrootRuntimeStateIsCleanedUpAndTheImageArtifactRemains(ctx context.Context) error {
	if err := s.waitForRuntimeStateCleanedUpAndArtifactRemains(ctx); err != nil {
		return err
	}
	statValue, err := s.squashStat(ctx)
	if err != nil {
		return err
	}
	if statValue == "" {
		return fmt.Errorf("tracked squashfs stat is empty")
	}
	s.squashStatBefore = statValue
	return nil
}

func (s *EnrootContainers) theSameEnrootContainerJobIsSubmittedAgain(ctx context.Context) error {
	if s.squashPath == "" {
		return fmt.Errorf("squashfs path is not captured")
	}
	if err := s.submitEnrootLifecycleJob(ctx, "e2e-enroot-repeated"); err != nil {
		return err
	}
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job,
		s.slurm.WaitForJobRunning(ctx, s.job.ID, enrootJobStartTimeout))
}

func (s *EnrootContainers) theExistingEnrootImageArtifactIsReused(ctx context.Context) error {
	if s.squashPath == "" {
		return fmt.Errorf("squashfs path is not captured")
	}
	if s.squashStatBefore == "" {
		return fmt.Errorf("baseline squashfs stat is not captured")
	}

	directSquashFS, err := s.enrootDirectSquashFSEnabled(ctx)
	if err != nil {
		return err
	}
	if directSquashFS {
		return s.enrootSquashfsArtifactIsReusedWithDirectMount(ctx)
	}
	return s.enrootSquashfsArtifactIsReusedWithMaterializedRuntime(ctx)
}

func (s *EnrootContainers) theRepeatedEnrootContainerJobIsCancelled(ctx context.Context) error {
	return s.cancelCurrentJob(ctx)
}

func (s *EnrootContainers) enrootRuntimeStateIsCleanedUp(ctx context.Context) error {
	return s.waitForRuntimeStateCleanedUpAndArtifactRemains(ctx)
}

func (s *EnrootContainers) anEnrootGPUSmokeJobIsSubmittedOnOneGPUWorker(ctx context.Context) error {
	// Future option: omit --nodelist and let Slurm choose the GPU worker.
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.workers = workers
	s.connectionWorker = workers[0]

	wrap := fmt.Sprintf("NVIDIA_DRIVER_CAPABILITIES=utility srun --container-image=%s nvidia-smi -L",
		framework.ShellQuote(gpuSmokeEnrootImage),
	)
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-enroot-gpu-smoke",
		Nodes:        1,
		Nodelist:     framework.WorkerNames(s.workers),
		GPUsPerNode:  1,
		TasksPerNode: 1,
		Wrap:         wrap,
	})
	if err != nil {
		return err
	}
	s.job = job
	s.runtime.Logf("enroot GPU smoke: selected worker=%s job_id=%s stdout=%s stderr=%s",
		s.connectionWorker.Name, job.ID, job.StdoutPath, job.StderrPath)
	return nil
}

func (s *EnrootContainers) theEnrootGPUSmokeJobSucceedsAndReportsVisibleGPUs(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("enroot GPU smoke job ID is empty")
	}
	job := s.job
	if err := waitForJobSucceeded(ctx, s.runtime, s.slurm, job, enrootGPUSmokeTimeout); err != nil {
		return err
	}
	if err := assertJobStdoutReportsVisibleGPUs(ctx, s.runtime, job); err != nil {
		return err
	}
	s.job = framework.SbatchJob{}
	return nil
}

func (s *EnrootContainers) submitEnrootLifecycleJob(ctx context.Context, jobName string) error {
	if len(s.workers) == 0 {
		return fmt.Errorf("enroot workers are not selected")
	}
	if s.connectionWorker.Name == "" {
		s.connectionWorker = s.workers[0]
	}

	wrap := fmt.Sprintf("srun --container-image=%s %s",
		framework.ShellQuote(enrootLifecycleImage),
		framework.BashLC(enrootLifecycleCommand),
	)
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      jobName,
		Nodes:        2,
		Nodelist:     framework.WorkerNames(s.workers),
		TasksPerNode: 1,
		Wrap:         wrap,
	})
	if err != nil {
		return err
	}
	s.job = job
	s.runtimeNamePrefix = fmt.Sprintf("pyxis_%s.", job.ID)
	s.runtime.Logf("enroot containers: submitted job=%s id=%s stdout=%s stderr=%s",
		job.JobName, job.ID, job.StdoutPath, job.StderrPath)
	return nil
}

func (s *EnrootContainers) waitForRuntimeStateCleanedUpAndArtifactRemains(ctx context.Context) error {
	if s.squashPath == "" {
		return fmt.Errorf("squashfs path is not captured")
	}
	if s.connectionWorker.Name == "" {
		return fmt.Errorf("enroot connection worker is not selected")
	}

	directSquashFS, err := s.enrootDirectSquashFSEnabled(ctx)
	if err != nil {
		return err
	}
	if directSquashFS {
		return s.waitForDirectRuntimeCleanedUpAndArtifactRemains(ctx)
	}
	return s.waitForMaterializedRuntimeCleanedUpAndArtifactRemains(ctx)
}

func (s *EnrootContainers) waitForMaterializedRuntimeCleanedUpAndArtifactRemains(ctx context.Context) error {
	runtimePrefix, err := s.enrootRuntimeNamePrefix()
	if err != nil {
		return err
	}
	return s.runtime.WaitFor(ctx, "enroot runtime data cleaned and image artifact remains", enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		treeOutput, err := s.legacyEnrootDataTree(waitCtx, s.connectionWorker)
		if err != nil {
			return false, err
		}
		if strings.Contains(treeOutput, runtimePrefix) {
			return false, nil
		}
		statValue, err := s.squashStat(waitCtx)
		if err != nil {
			return false, err
		}
		return statValue != "", nil
	})
}

func (s *EnrootContainers) waitForDirectRuntimeCleanedUpAndArtifactRemains(ctx context.Context) error {
	return s.runtime.WaitFor(ctx, "enroot direct squashfs unmounted and image artifact remains", enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		mounted, err := s.squashfsMountProcessExists(waitCtx)
		if err != nil {
			return false, err
		}
		if mounted {
			return false, nil
		}
		statValue, err := s.squashStat(waitCtx)
		if err != nil {
			return false, err
		}
		return statValue != "", nil
	})
}

func (s *EnrootContainers) enrootRuntimeContainerDataIsVisibleWhileTheJobIsRunning(ctx context.Context) error {
	if s.connectionWorker.Name == "" {
		return fmt.Errorf("enroot connection worker is not selected")
	}
	runtimePrefix, err := s.enrootRuntimeNamePrefix()
	if err != nil {
		return err
	}

	err = framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, s.job, "enroot runtime container visible",
		enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			listOutput, err := s.runtime.Worker(s.connectionWorker).RunWithDefaultRetry(waitCtx, "sudo enroot list || true")
			if err != nil {
				return false, err
			}
			treeOutput, err := s.legacyEnrootDataTree(waitCtx, s.connectionWorker)
			if err != nil {
				return false, err
			}
			if !strings.Contains(listOutput, runtimePrefix) {
				return false, nil
			}
			if !strings.Contains(treeOutput, runtimePrefix) {
				return false, nil
			}
			return true, nil
		})
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job, err)
}

func (s *EnrootContainers) enrootSquashfsImageIsMountedDirectlyWhileTheJobIsRunning(ctx context.Context) error {
	if s.connectionWorker.Name == "" {
		return fmt.Errorf("enroot connection worker is not selected")
	}
	if s.squashPath == "" {
		return fmt.Errorf("squashfs path is not captured")
	}

	err := framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, s.job, "enroot squashfs mounted directly",
		enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			mounted, err := s.squashfsMountProcessExists(waitCtx)
			if err != nil {
				return false, err
			}
			return mounted, nil
		})
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job, err)
}

func (s *EnrootContainers) enrootSquashfsArtifactIsReusedWithMaterializedRuntime(ctx context.Context) error {
	runtimePrefix, err := s.enrootRuntimeNamePrefix()
	if err != nil {
		return err
	}
	err = framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, s.job, "enroot image artifact reused with materialized runtime",
		enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			treeOutput, err := s.legacyEnrootDataTree(waitCtx, s.connectionWorker)
			if err != nil {
				return false, err
			}
			if !strings.Contains(treeOutput, runtimePrefix) {
				return false, nil
			}
			statOutput, err := s.squashStat(waitCtx)
			if err != nil {
				return false, err
			}
			return statOutput == s.squashStatBefore, nil
		})
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job, err)
}

func (s *EnrootContainers) enrootSquashfsArtifactIsReusedWithDirectMount(ctx context.Context) error {
	err := framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, s.job, "enroot image artifact reused with direct squashfs mount",
		enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			mounted, err := s.squashfsMountProcessExists(waitCtx)
			if err != nil {
				return false, err
			}
			if !mounted {
				return false, nil
			}
			statOutput, err := s.squashStat(waitCtx)
			if err != nil {
				return false, err
			}
			return statOutput == s.squashStatBefore, nil
		})
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job, err)
}

func (s *EnrootContainers) cancelCurrentJob(ctx context.Context) error {
	if s.job.IsZero() {
		return nil
	}

	jobID := s.job.ID
	if err := s.slurm.CancelJob(ctx, jobID, enrootStopTimeout); err != nil {
		return fmt.Errorf("cancel enroot job %s: %w", jobID, err)
	}
	return nil
}

func (s *EnrootContainers) expectedSquashArtifactExists(ctx context.Context) (bool, error) {
	if s.expectedSquashPath == "" {
		expectedSquashPath, err := s.expectedLifecycleSquashPath(ctx)
		if err != nil {
			return false, err
		}
		s.expectedSquashPath = expectedSquashPath
	}

	exists, err := s.pathExists(ctx, s.expectedSquashPath)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (s *EnrootContainers) expectedLifecycleSquashPath(ctx context.Context) (string, error) {
	digest, err := s.enrootImageDigest(ctx, enrootLifecycleImage)
	if err != nil {
		return "", err
	}
	return path.Join(enrootSquashRoot, digest+".sqsh"), nil
}

func (s *EnrootContainers) enrootImageDigest(ctx context.Context, image string) (string, error) {
	if s.connectionWorker.Name == "" {
		return "", fmt.Errorf("enroot connection worker is not selected")
	}

	output, err := s.runtime.Worker(s.connectionWorker).RunWithDefaultRetry(ctx,
		fmt.Sprintf("enroot digest %s", framework.ShellQuote(image)))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		candidate := strings.TrimSpace(line)
		if strings.HasPrefix(candidate, "sha256:") {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("digest for enroot image %s not found in output:\n%s", image, strings.TrimSpace(output))
}

func (s *EnrootContainers) pathExists(ctx context.Context, targetPath string) (bool, error) {
	output, err := s.runtime.Worker(s.connectionWorker).RunWithDefaultRetry(ctx,
		fmt.Sprintf("sudo test -f %s && echo exists || true", framework.ShellQuote(targetPath)))
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) == "exists", nil
}

func (s *EnrootContainers) squashfsMountProcessExists(ctx context.Context) (bool, error) {
	output, err := s.runtime.Worker(s.connectionWorker).RunWithDefaultRetry(ctx,
		fmt.Sprintf("sudo ps -eww -o args= | grep -F -- %s | grep -F -- squashfuse | grep -v grep || true", framework.ShellQuote(s.squashPath)))
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

func (s *EnrootContainers) enrootDirectSquashFSEnabled(ctx context.Context) (bool, error) {
	if s.directSquashFS != nil {
		return *s.directSquashFS, nil
	}

	output, err := s.runtime.Jail().RunWithDefaultRetry(ctx, "grep -E 'spank_pyxis\\.so.*use_squashfuse=1' /etc/slurm/plugstack.conf /etc/slurm/plugstack.conf.d/*.conf 2>/dev/null || true")
	if err != nil {
		return false, err
	}
	enabled := strings.TrimSpace(output) != ""
	s.directSquashFS = &enabled
	return enabled, nil
}

func (s *EnrootContainers) squashStat(ctx context.Context) (string, error) {
	return s.squashStatForPath(ctx, s.squashPath)
}

func (s *EnrootContainers) squashStatForPath(ctx context.Context, squashPath string) (string, error) {
	statOutput, err := s.runtime.Worker(s.connectionWorker).RunWithDefaultRetry(ctx, fmt.Sprintf("sudo stat -c '%%Y:%%s' %s", framework.ShellQuote(squashPath)))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(statOutput), nil
}

func (s *EnrootContainers) legacyEnrootDataTree(ctx context.Context, worker framework.WorkerInfo) (string, error) {
	return s.runtime.Worker(worker).RunWithDefaultRetry(ctx, fmt.Sprintf("sudo tree -L 1 %s", framework.ShellQuote(enrootDedicatedDataPath)))
}

func (s *EnrootContainers) enrootRuntimeNamePrefix() (string, error) {
	if s.runtimeNamePrefix == "" {
		return "", fmt.Errorf("enroot runtime name prefix is not captured")
	}
	return s.runtimeNamePrefix, nil
}
