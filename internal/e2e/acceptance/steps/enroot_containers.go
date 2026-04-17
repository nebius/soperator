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

	enrootDockerImage   = "docker://cr.eu-north1.nebius.cloud#soperator/active_checks:12.9.0-ubuntu24.04-nccl_tests2.16.4-3935b93"
	enrootDockerMount   = "/usr/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu,/usr/lib64:/usr/lib64"
	enrootARP           = "all_reduce_perf_mpi -b 8G -e 8G -f 2 -g 1 -N 0"
	enrootNamedJobName  = "kek"
	enrootSquashPattern = ".sqsh"
	enrootSquashRoot    = "/mnt/jail/var/cache/enroot-container-images"

	enrootJobStartTimeout = 25 * time.Minute
	enrootProbeTimeout    = 10 * time.Minute
	enrootStopTimeout     = 5 * time.Minute
)

type EnrootContainers struct {
	exec framework.Exec

	workers []string
	jobID   string

	squashPath       string
	squashStatBefore string
}

func NewEnrootContainers(exec framework.Exec) *EnrootContainers {
	return &EnrootContainers{exec: exec}
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
	if s.jobID == "" {
		return fmt.Errorf("enroot job id is empty")
	}
	return waitForJobRunning(ctx, s.exec, s.jobID, enrootJobStartTimeout)
}

func (s *EnrootContainers) enrootCacheIsPopulatedOnLocalStorageOnAWorker(ctx context.Context) error {
	return s.waitForTreeEntriesOnWorker(ctx, "/mnt/image-storage/enroot/cache/", "enroot cache is populated")
}

func (s *EnrootContainers) enrootSquashfsImageIsPresentOnAWorker(ctx context.Context) error {
	worker := s.firstWorker()
	if worker == "" {
		return fmt.Errorf("enroot workers are not selected")
	}

	return s.exec.WaitFor(ctx, "enroot squashfs image present", enrootProbeTimeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
		treeOutput, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker,
			fmt.Sprintf("sudo tree -L 4 -hug %s 2>/dev/null || true", framework.ShellQuote(enrootSquashRoot)))
		if err != nil {
			return false, err
		}
		if !strings.Contains(treeOutput, enrootSquashPattern) {
			return false, nil
		}

		findOutput, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker,
			fmt.Sprintf("sudo find %s -type f -name '*%s' 2>/dev/null | sort", framework.ShellQuote(enrootSquashRoot), enrootSquashPattern))
		if err != nil {
			return false, err
		}
		squashPath := pickSquashPath(findOutput)
		if squashPath == "" {
			return false, nil
		}
		s.squashPath = squashPath
		return true, nil
	})
}

func (s *EnrootContainers) enrootRuntimeContainerDataIsVisibleWhileTheJobIsRunning(ctx context.Context) error {
	worker := s.firstWorker()
	if worker == "" {
		return fmt.Errorf("enroot workers are not selected")
	}

	return s.exec.WaitFor(ctx, "enroot runtime container visible", enrootProbeTimeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
		listOutput, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker, "sudo enroot list || true")
		if err != nil {
			return false, err
		}
		treeOutput, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker, "sudo tree -L 1 /mnt/image-storage/enroot/data/")
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
}

func (s *EnrootContainers) theEnrootNCCLJobIsCancelled(ctx context.Context) error {
	return s.cancelCurrentJob(ctx)
}

func (s *EnrootContainers) enrootRuntimeDataIsCleanedUpAndSquashfsCacheRemains(ctx context.Context) error {
	if s.squashPath == "" {
		return fmt.Errorf("squashfs path is not captured")
	}

	worker := s.firstWorker()
	if worker == "" {
		return fmt.Errorf("enroot workers are not selected")
	}

	err := s.exec.WaitFor(ctx, "enroot runtime data cleaned and squashfs cache remains", enrootProbeTimeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
		treeOutput, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker, "sudo tree -L 1 /mnt/image-storage/enroot/data/")
		if err != nil {
			return false, err
		}
		if strings.Contains(treeOutput, "pyxis_") {
			return false, nil
		}
		if _, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker, fmt.Sprintf("sudo test -f %s", framework.ShellQuote(s.squashPath))); err != nil {
			return false, err
		}
		statOutput, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker, fmt.Sprintf("sudo stat -c '%%Y:%%s' %s", framework.ShellQuote(s.squashPath)))
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
	if err != nil {
		return err
	}
	return nil
}

func (s *EnrootContainers) theSameEnrootNCCLJobIsSubmittedAgain(ctx context.Context) error {
	if err := s.submitEnrootJob(ctx, "", "e2e-enroot-repeated"); err != nil {
		return err
	}
	return waitForJobRunning(ctx, s.exec, s.jobID, enrootJobStartTimeout)
}

func (s *EnrootContainers) enrootRuntimeDataIsRepopulatedWithoutChangingTheSquashfsArtifact(ctx context.Context) error {
	if s.squashPath == "" {
		return fmt.Errorf("squashfs path is not captured")
	}
	if s.squashStatBefore == "" {
		return fmt.Errorf("baseline squashfs stat is not captured")
	}

	worker := s.firstWorker()
	if worker == "" {
		return fmt.Errorf("enroot workers are not selected")
	}

	return s.exec.WaitFor(ctx, "enroot data repopulated from cache", enrootProbeTimeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
		treeOutput, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker, "sudo tree -L 1 /mnt/image-storage/enroot/data/")
		if err != nil {
			return false, err
		}
		if !strings.Contains(treeOutput, "pyxis_") {
			return false, nil
		}

		statOutput, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker, fmt.Sprintf("sudo stat -c '%%Y:%%s' %s", framework.ShellQuote(s.squashPath)))
		if err != nil {
			return false, err
		}
		return strings.TrimSpace(statOutput) == s.squashStatBefore, nil
	})
}

func (s *EnrootContainers) theRepeatedEnrootNCCLJobIsCancelled(ctx context.Context) error {
	return s.cancelCurrentJob(ctx)
}

func (s *EnrootContainers) aNamedEnrootContainerJobIsSubmitted(ctx context.Context) error {
	if err := s.submitEnrootJob(ctx, enrootNamedJobName, "e2e-enroot-named"); err != nil {
		return err
	}
	if err := waitForJobRunning(ctx, s.exec, s.jobID, enrootJobStartTimeout); err != nil {
		return err
	}
	return s.cancelCurrentJob(ctx)
}

func (s *EnrootContainers) theNamedEnrootRuntimeDirectoryRemainsAfterCancellation(ctx context.Context) error {
	namedDir := namedEnrootDir(enrootNamedJobName)

	return s.exec.WaitFor(ctx, "named enroot runtime directory remains", enrootProbeTimeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
		for _, worker := range s.workers {
			treeOutput, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker, "sudo tree -L 1 /mnt/image-storage/enroot/data/")
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
	submitCmd := fmt.Sprintf(
		"sbatch --parsable -N 2 --nodelist=%s --ntasks-per-node=1 --job-name=e2e-enroot-cleanup --wrap=%s",
		framework.ShellQuote(strings.Join(s.workers, ",")),
		framework.ShellQuote(cleanupWrap),
	)
	out, err := s.exec.ExecJail(ctx, submitCmd)
	if err != nil {
		return fmt.Errorf("submit named enroot cleanup job: %w", err)
	}
	cleanupJobID, err := parseSbatchJobID(out)
	if err != nil {
		return fmt.Errorf("parse named enroot cleanup job id: %w", err)
	}
	return waitForJobGone(ctx, s.exec, cleanupJobID, enrootStopTimeout)
}

func (s *EnrootContainers) theNamedEnrootRuntimeDirectoryIsRemoved(ctx context.Context) error {
	namedDir := namedEnrootDir(enrootNamedJobName)
	return s.exec.WaitFor(ctx, "named enroot runtime directory removed", enrootProbeTimeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
		for _, worker := range s.workers {
			treeOutput, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker, "sudo tree -L 1 /mnt/image-storage/enroot/data/")
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
		workers, err := selectGPUWorkers(ctx, s.exec, 2)
		if err != nil {
			return err
		}
		s.workers = workers
		s.exec.Logf("enroot containers: selected workers=%s", strings.Join(workers, ","))
	}

	wrap := fmt.Sprintf("srun --mpi=pmix --container-image=%s --container-mounts=%s %s",
		framework.ShellQuote(enrootDockerImage),
		framework.ShellQuote(enrootDockerMount),
		enrootARP,
	)
	if containerName != "" {
		wrap = fmt.Sprintf("srun --mpi=pmix --container-image=%s --container-name=%s --container-mounts=%s %s",
			framework.ShellQuote(enrootDockerImage),
			framework.ShellQuote(containerName),
			framework.ShellQuote(enrootDockerMount),
			enrootARP,
		)
	}

	submit := fmt.Sprintf(
		"sbatch --parsable -N 2 --nodelist=%s --gpus-per-node=8 --ntasks-per-node=8 --job-name=%s --wrap=%s",
		framework.ShellQuote(strings.Join(s.workers, ",")),
		framework.ShellQuote(jobName),
		framework.ShellQuote(wrap),
	)
	out, err := s.exec.ExecJail(ctx, submit)
	if err != nil {
		return fmt.Errorf("submit enroot job %q: %w", jobName, err)
	}
	jobID, err := parseSbatchJobID(out)
	if err != nil {
		return fmt.Errorf("parse enroot job id for %q: %w", jobName, err)
	}
	s.jobID = jobID
	s.exec.Logf("enroot containers: submitted job=%s id=%s", jobName, jobID)
	return nil
}

func (s *EnrootContainers) cancelCurrentJob(ctx context.Context) error {
	if s.jobID == "" {
		return nil
	}

	jobID := s.jobID
	if err := cancelSlurmJob(ctx, s.exec, jobID, enrootStopTimeout); err != nil {
		return fmt.Errorf("cancel enroot job %s: %w", jobID, err)
	}
	s.jobID = ""
	return nil
}

func (s *EnrootContainers) waitForTreeEntriesOnWorker(ctx context.Context, storagePath, description string) error {
	worker := s.firstWorker()
	if worker == "" {
		return fmt.Errorf("enroot workers are not selected")
	}

	return s.exec.WaitFor(ctx, description, enrootProbeTimeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
		out, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker, fmt.Sprintf("sudo tree -L 2 -a %s", framework.ShellQuote(storagePath)))
		if err != nil {
			return false, err
		}
		return treeOutputHasEntries(out), nil
	})
}

func (s *EnrootContainers) firstWorker() string {
	if len(s.workers) == 0 {
		return ""
	}
	return s.workers[0]
}

func (s *EnrootContainers) removeNamedRuntimeDir(ctx context.Context) error {
	if len(s.workers) == 0 {
		return nil
	}
	var failures []string
	for _, worker := range s.workers {
		_, err := runWorkerCommand(ctx, s.exec, worker, fmt.Sprintf("sudo rm -rf %s", framework.ShellQuote(namedEnrootDirPath(enrootNamedJobName))))
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
