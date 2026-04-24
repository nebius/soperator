package steps

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	enrootContainersFeatureFile = "enroot_containers.feature"

	enrootDockerMount   = "/usr/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu,/usr/lib64:/usr/lib64"
	enrootARP           = "NCCL_P2P_DISABLE=1 NCCL_SHM_DISABLE=1 NCCL_ALGO=Ring all_reduce_perf -b 8G -e 8G -f 2 -g 8 -N 0"
	enrootNamedJobName  = "kek"
	enrootSquashPattern = ".sqsh"
	enrootSquashRoot    = "/var/cache/enroot-container-images"
	enrootTasksPerNode  = 8

	enrootJobStartTimeout = 25 * time.Minute
	enrootProbeTimeout    = 10 * time.Minute
	enrootStopTimeout     = 5 * time.Minute
)

type EnrootContainers struct {
	exec  framework.Exec
	slurm *framework.SlurmClient

	workers          []string
	connectionWorker string
	job              framework.SbatchJob

	squashPath       string
	squashStatBefore string
}

func NewEnrootContainers(exec framework.Exec, slurm *framework.SlurmClient) *EnrootContainers {
	return &EnrootContainers{
		exec:  exec,
		slurm: slurm,
	}
}

func (s *EnrootContainers) Register(sc *godog.ScenarioContext) {
	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		if path.Base(scenario.Uri) != enrootContainersFeatureFile {
			return ctx, nil
		}

		if cleanupErr := s.cancelCurrentJob(context.Background()); cleanupErr != nil {
			s.exec.Logf("cleanup: cancel enroot job: %v", cleanupErr)
		}
		if cleanupErr := s.removeNamedRuntimeDir(context.Background()); cleanupErr != nil {
			s.exec.Logf("cleanup: remove named enroot runtime directory: %v", cleanupErr)
		}
		return ctx, nil
	})

	sc.Step(`^a long-running Enroot NCCL job is submitted on two GPU workers$`, s.aLongRunningEnrootNCCLJobIsSubmittedOnTwoGPUWorkers)
	sc.Step(`^the Enroot NCCL job is running$`, s.theEnrootNCCLJobIsRunning)
	sc.Step(`^Enroot cache is populated on local storage on a worker$`, s.enrootCacheIsPopulatedOnLocalStorageOnAWorker)
	sc.Step(`^Enroot squashfs image is present on a worker$`, s.enrootSquashfsImageIsPresentOnAWorker)
	sc.Step(`^Enroot runtime container data is visible while the job is running$`, s.enrootRuntimeContainerDataIsVisibleWhileTheJobIsRunning)
	sc.Step(`^the Enroot NCCL job is cancelled$`, s.theEnrootNCCLJobIsCancelled)
	sc.Step(`^Enroot runtime data is cleaned up and squashfs cache remains$`, s.enrootRuntimeDataIsCleanedUpAndSquashfsCacheRemains)
	sc.Step(`^the same Enroot NCCL job is submitted again$`, s.theSameEnrootNCCLJobIsSubmittedAgain)
	sc.Step(`^Enroot runtime data is repopulated without changing the squashfs artifact$`, s.enrootRuntimeDataIsRepopulatedWithoutChangingTheSquashfsArtifact)
	sc.Step(`^the repeated Enroot NCCL job is cancelled$`, s.theRepeatedEnrootNCCLJobIsCancelled)
	sc.Step(`^a named Enroot container job is submitted$`, s.aNamedEnrootContainerJobIsSubmitted)
	sc.Step(`^the named Enroot runtime directory remains after cancellation$`, s.theNamedEnrootRuntimeDirectoryRemainsAfterCancellation)
	sc.Step(`^the named Enroot runtime directory is cleaned up$`, s.theNamedEnrootRuntimeDirectoryIsCleanedUp)
	sc.Step(`^the named Enroot runtime directory is removed$`, s.theNamedEnrootRuntimeDirectoryIsRemoved)
}

func (s *EnrootContainers) aLongRunningEnrootNCCLJobIsSubmittedOnTwoGPUWorkers(ctx context.Context) error {
	return s.submitEnrootJob(ctx, "", "e2e-enroot-initial")
}

func (s *EnrootContainers) theEnrootNCCLJobIsRunning(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("enroot job id is empty")
	}
	return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, s.job,
		s.slurm.WaitForJobRunning(ctx, s.job.ID, enrootJobStartTimeout))
}

func (s *EnrootContainers) enrootCacheIsPopulatedOnLocalStorageOnAWorker(ctx context.Context) error {
	return framework.WaitForTreeEntriesOnWorker(ctx, s.exec, s.connectionWorker, "/mnt/image-storage/enroot/cache/", "enroot cache is populated", enrootProbeTimeout)
}

func (s *EnrootContainers) enrootSquashfsImageIsPresentOnAWorker(ctx context.Context) error {
	if s.connectionWorker == "" {
		return fmt.Errorf("enroot connection worker is not selected")
	}

	if err := s.exec.WaitFor(ctx, "enroot squashfs image present", enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		findOutput, err := s.exec.Worker(s.connectionWorker).RunWithDefaultRetry(waitCtx,
			fmt.Sprintf("sudo find %s -type f -name '*%s' 2>/dev/null | sort", framework.ShellQuote(enrootSquashRoot), enrootSquashPattern))
		if err != nil {
			return false, err
		}
		squashPath := pickSquashPath(findOutput)
		if squashPath == "" {
			return false, nil
		}
		s.squashPath = squashPath
		s.exec.Logf("enroot containers: tracked squashfs path=%s", squashPath)
		return true, nil
	}); err != nil {
		return err
	}
	return nil
}

func (s *EnrootContainers) enrootRuntimeContainerDataIsVisibleWhileTheJobIsRunning(ctx context.Context) error {
	if s.connectionWorker == "" {
		return fmt.Errorf("enroot connection worker is not selected")
	}

	err := framework.WaitForWithJobAlive(ctx, s.exec, s.slurm, s.job, "enroot runtime container visible",
		enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			listOutput, err := s.exec.Worker(s.connectionWorker).RunWithDefaultRetry(waitCtx, "sudo enroot list || true")
			if err != nil {
				return false, err
			}
			treeOutput, err := s.exec.Worker(s.connectionWorker).RunWithDefaultRetry(waitCtx, "sudo tree -L 1 /mnt/image-storage/enroot/data/")
			if err != nil {
				return false, err
			}
			if !strings.Contains(listOutput, "pyxis_") {
				return false, nil
			}
			if !strings.Contains(treeOutput, "pyxis_") {
				return false, nil
			}
			return true, nil
		})
	return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, s.job, err)
}

func (s *EnrootContainers) theEnrootNCCLJobIsCancelled(ctx context.Context) error {
	return s.cancelCurrentJob(ctx)
}

func (s *EnrootContainers) enrootRuntimeDataIsCleanedUpAndSquashfsCacheRemains(ctx context.Context) error {
	if s.squashPath == "" {
		return fmt.Errorf("squashfs path is not captured")
	}
	if s.connectionWorker == "" {
		return fmt.Errorf("enroot connection worker is not selected")
	}

	err := s.exec.WaitFor(ctx, "enroot runtime data cleaned and squashfs cache remains", enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		treeOutput, err := s.exec.Worker(s.connectionWorker).RunWithDefaultRetry(waitCtx, "sudo tree -L 1 /mnt/image-storage/enroot/data/")
		if err != nil {
			return false, err
		}
		if strings.Contains(treeOutput, "pyxis_") {
			return false, nil
		}
		if _, err := s.exec.Worker(s.connectionWorker).RunWithDefaultRetry(waitCtx, fmt.Sprintf("sudo test -f %s", framework.ShellQuote(s.squashPath))); err != nil {
			return false, err
		}
		statOutput, err := s.exec.Worker(s.connectionWorker).RunWithDefaultRetry(waitCtx, fmt.Sprintf("sudo stat -c '%%Y:%%s' %s", framework.ShellQuote(s.squashPath)))
		if err != nil {
			return false, err
		}
		statValue := strings.TrimSpace(statOutput)
		if statValue == "" {
			return false, nil
		}
		s.squashStatBefore = statValue
		return true, nil
	})
	return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, s.job, err)
}

func (s *EnrootContainers) theSameEnrootNCCLJobIsSubmittedAgain(ctx context.Context) error {
	if err := s.submitEnrootJob(ctx, "", "e2e-enroot-repeated"); err != nil {
		return err
	}
	return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, s.job,
		s.slurm.WaitForJobRunning(ctx, s.job.ID, enrootJobStartTimeout))
}

func (s *EnrootContainers) enrootRuntimeDataIsRepopulatedWithoutChangingTheSquashfsArtifact(ctx context.Context) error {
	if s.squashPath == "" {
		return fmt.Errorf("squashfs path is not captured")
	}
	if s.squashStatBefore == "" {
		return fmt.Errorf("baseline squashfs stat is not captured")
	}
	if s.connectionWorker == "" {
		return fmt.Errorf("enroot connection worker is not selected")
	}

	err := framework.WaitForWithJobAlive(ctx, s.exec, s.slurm, s.job, "enroot data repopulated from cache",
		enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			// We validate cache reuse by checking the tracked squashfs stat is unchanged
			// while runtime data is recreated. We do not compare full directory trees.
			treeOutput, err := s.exec.Worker(s.connectionWorker).RunWithDefaultRetry(waitCtx, "sudo tree -L 1 /mnt/image-storage/enroot/data/")
			if err != nil {
				return false, err
			}
			if !strings.Contains(treeOutput, "pyxis_") {
				return false, nil
			}

			statOutput, err := s.exec.Worker(s.connectionWorker).RunWithDefaultRetry(waitCtx, fmt.Sprintf("sudo stat -c '%%Y:%%s' %s", framework.ShellQuote(s.squashPath)))
			if err != nil {
				return false, err
			}
			return strings.TrimSpace(statOutput) == s.squashStatBefore, nil
		})
	return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, s.job, err)
}

func (s *EnrootContainers) theRepeatedEnrootNCCLJobIsCancelled(ctx context.Context) error {
	return s.cancelCurrentJob(ctx)
}

func (s *EnrootContainers) aNamedEnrootContainerJobIsSubmitted(ctx context.Context) error {
	if err := s.submitEnrootJob(ctx, enrootNamedJobName, "e2e-enroot-named"); err != nil {
		return err
	}
	if err := framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, s.job,
		s.slurm.WaitForJobRunning(ctx, s.job.ID, enrootJobStartTimeout)); err != nil {
		return err
	}
	namedDir := namedEnrootDir(enrootNamedJobName)
	err := framework.WaitForWithJobAlive(ctx, s.exec, s.slurm, s.job, "named enroot runtime directory created",
		enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			for _, worker := range s.workers {
				treeOutput, err := s.exec.Worker(worker).RunWithDefaultRetry(waitCtx, "sudo tree -L 1 /mnt/image-storage/enroot/data/")
				if err != nil {
					return false, err
				}
				if !strings.Contains(treeOutput, namedDir) {
					return false, nil
				}
			}
			return true, nil
		})
	if err := framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, s.job, err); err != nil {
		return err
	}
	return s.cancelCurrentJob(ctx)
}

func (s *EnrootContainers) theNamedEnrootRuntimeDirectoryRemainsAfterCancellation(ctx context.Context) error {
	namedDir := namedEnrootDir(enrootNamedJobName)

	return s.exec.WaitFor(ctx, "named enroot runtime directory remains", enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		for _, worker := range s.workers {
			treeOutput, err := s.exec.Worker(worker).RunWithDefaultRetry(waitCtx, "sudo tree -L 1 /mnt/image-storage/enroot/data/")
			if err != nil {
				return false, err
			}
			if !strings.Contains(treeOutput, namedDir) {
				return false, nil
			}
		}
		return true, nil
	})
}

func (s *EnrootContainers) theNamedEnrootRuntimeDirectoryIsCleanedUp(ctx context.Context) error {
	if len(s.workers) == 0 {
		return fmt.Errorf("enroot workers are not selected")
	}

	cleanupWrap := fmt.Sprintf("srun rm -rf %s", framework.ShellQuote(namedEnrootDirPath(enrootNamedJobName)))
	cleanup, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-enroot-cleanup",
		Nodes:        2,
		Nodelist:     s.workers,
		TasksPerNode: 1,
		Wrap:         cleanupWrap,
	})
	if err != nil {
		return err
	}
	s.exec.Logf("enroot containers: submitted cleanup job id=%s stdout=%s stderr=%s",
		cleanup.ID, cleanup.StdoutPath, cleanup.StderrPath)
	return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, cleanup,
		s.slurm.WaitForJobGone(ctx, cleanup.ID, enrootStopTimeout))
}

func (s *EnrootContainers) theNamedEnrootRuntimeDirectoryIsRemoved(ctx context.Context) error {
	namedDir := namedEnrootDir(enrootNamedJobName)
	return s.exec.WaitFor(ctx, "named enroot runtime directory removed", enrootProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		for _, worker := range s.workers {
			treeOutput, err := s.exec.Worker(worker).RunWithDefaultRetry(waitCtx, "sudo tree -L 1 /mnt/image-storage/enroot/data/")
			if err != nil {
				return false, err
			}
			if strings.Contains(treeOutput, namedDir) {
				return false, nil
			}
		}
		return true, nil
	})
}

func (s *EnrootContainers) submitEnrootJob(ctx context.Context, containerName, jobName string) error {
	if len(s.workers) == 0 {
		workers, err := s.slurm.AnyGPUWorkers(2)
		if err != nil {
			return err
		}
		s.workers = workers
		s.connectionWorker = s.workers[0]
		s.exec.Logf("enroot containers: selected workers=%s", strings.Join(s.workers, ","))
	} else if s.connectionWorker == "" {
		s.connectionWorker = s.workers[0]
	}

	image, err := framework.RequiredEnv("E2E_ENROOT_IMAGE")
	if err != nil {
		return err
	}

	// Wrap the NCCL body in `bash -lc` so env assignments (NCCL_*=…) are
	// shell‑parsed inside the container instead of being passed as positional
	// args to srun / pyxis, where execve() treats them as the program name and
	// fails with "No such file or directory". Mirrors docker_containers.go.
	wrap := fmt.Sprintf("srun --mpi=pmix --container-image=%s --container-mounts=%s bash -lc %s",
		framework.ShellQuote(image),
		framework.ShellQuote(enrootDockerMount),
		framework.ShellQuote(enrootARP),
	)
	if containerName != "" {
		wrap = fmt.Sprintf("srun --mpi=pmix --container-image=%s --container-name=%s --container-mounts=%s bash -lc %s",
			framework.ShellQuote(image),
			framework.ShellQuote(containerName),
			framework.ShellQuote(enrootDockerMount),
			framework.ShellQuote(enrootARP),
		)
	}

	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      jobName,
		Nodes:        2,
		Nodelist:     s.workers,
		GPUsPerNode:  8,
		TasksPerNode: enrootTasksPerNode,
		Wrap:         wrap,
	})
	if err != nil {
		return err
	}
	s.job = job
	s.exec.Logf("enroot containers: submitted job=%s id=%s stdout=%s stderr=%s",
		job.JobName, job.ID, job.StdoutPath, job.StderrPath)
	return nil
}

func (s *EnrootContainers) cancelCurrentJob(ctx context.Context) error {
	if s.job.IsZero() {
		return nil
	}

	jobID := s.job.ID
	if err := s.slurm.CancelJob(ctx, jobID, enrootStopTimeout); err != nil {
		return fmt.Errorf("cancel enroot job %s: %w", jobID, err)
	}
	s.job = framework.SbatchJob{}
	return nil
}

func (s *EnrootContainers) removeNamedRuntimeDir(ctx context.Context) error {
	if len(s.workers) == 0 {
		return nil
	}
	var failures []string
	for _, worker := range s.workers {
		_, err := s.exec.Worker(worker).Run(ctx, fmt.Sprintf("sudo rm -rf %s", framework.ShellQuote(namedEnrootDirPath(enrootNamedJobName))))
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", worker, err))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func pickSquashPath(findOutput string) string {
	var fallback string
	for _, line := range strings.Split(findOutput, "\n") {
		candidate := strings.TrimSpace(line)
		if candidate == "" {
			continue
		}
		if fallback == "" {
			fallback = candidate
		}
		if strings.Contains(candidate, enrootSquashPattern) {
			return candidate
		}
	}
	return fallback
}

func namedEnrootDir(containerName string) string {
	return "pyxis_" + containerName
}

func namedEnrootDirPath(containerName string) string {
	return "/mnt/image-storage/enroot/data/" + namedEnrootDir(containerName)
}
