package steps

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	dockerContainersFeatureFile = "docker_containers.feature"

	// Small mirrored image; Docker lifecycle only needs sh/sleep and storage population.
	dockerLifecycleImage   = "cr.eu-north1.nebius.cloud/soperator/busybox"
	dockerLifecycleCommand = "echo ready; sleep 3600"

	// Cluster-supported GPU diagnostic image; includes nvidia-smi for container GPU visibility.
	dockerGPUImage = "cr.eu-north1.nebius.cloud/ml-containers/training_diag:13.0.2-ubuntu24.04-20260709140028"

	dockerJobStartTimeout      = 20 * time.Minute
	dockerProbeTimeout         = 10 * time.Minute
	dockerJobCancelTimeout     = 5 * time.Minute
	dockerContainerStopTimeout = 5 * time.Minute
	dockerGPUSmokeTimeout      = 10 * time.Minute
)

type DockerContainers struct {
	exec    framework.Exec
	slurm   *framework.SlurmClient
	workers []string
	job     framework.SbatchJob

	containerNamePrefix string
	connectionWorker    string
}

func NewDockerContainers(exec framework.Exec, slurm *framework.SlurmClient) *DockerContainers {
	return &DockerContainers{
		exec:  exec,
		slurm: slurm,
	}
}

func (s *DockerContainers) Register(sc *godog.ScenarioContext) {
	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		if path.Base(scenario.Uri) != dockerContainersFeatureFile {
			return ctx, nil
		}

		if cleanupErr := s.requestCurrentJobCancellation(context.Background()); cleanupErr != nil {
			s.exec.Logf("cleanup: cancel Docker job: %v", cleanupErr)
		}
		s.stopContainersByNamePrefix(context.Background())
		s.removeContainersByNamePrefix(context.Background())
		if cleanupErr := s.waitForCurrentJobGone(context.Background()); cleanupErr != nil {
			s.exec.Logf("cleanup: wait for Docker job to finish: %v", cleanupErr)
		}
		return ctx, nil
	})

	sc.Step(`^a long-running Docker container job is submitted on two workers$`, s.aLongRunningDockerContainerJobIsSubmittedOnTwoWorkers)
	sc.Step(`^the Docker container job is running$`, s.theDockerContainerJobIsRunning)
	sc.Step(`^Docker image and runtime storage is populated on a worker$`, s.dockerImageAndRuntimeStorageIsPopulatedOnAWorker)
	sc.Step(`^Docker containers from the job are running on selected workers$`, s.dockerContainersFromTheJobAreRunningOnSelectedWorkers)
	sc.Step(`^the Docker container job is cancelled$`, s.theDockerContainerJobIsCancelled)
	sc.Step(`^Docker containers from the job are stopped explicitly$`, s.dockerContainersFromTheJobAreStoppedExplicitly)
	sc.Step(`^Docker containers from the job are no longer running$`, s.dockerContainersFromTheJobAreNoLongerRunning)
	sc.Step(`^a Docker GPU smoke job is submitted on one GPU worker$`, s.aDockerGPUSmokeJobIsSubmittedOnOneGPUWorker)
	sc.Step(`^the Docker GPU smoke job succeeds and reports visible GPUs$`, s.theDockerGPUSmokeJobSucceedsAndReportsVisibleGPUs)
}

func (s *DockerContainers) aLongRunningDockerContainerJobIsSubmittedOnTwoWorkers(ctx context.Context) error {
	workers, err := s.slurm.AnyWorkers(2)
	if err != nil {
		return err
	}
	s.workers = workers
	s.connectionWorker = workers[0]

	wrap := fmt.Sprintf("srun docker run --rm --name e2e-docker-${SLURM_JOB_ID}-${SLURM_NODEID} %s sh -c %s",
		framework.ShellQuote(dockerLifecycleImage),
		framework.ShellQuote(dockerLifecycleCommand),
	)
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-docker-containers",
		Nodes:        2,
		Nodelist:     s.workers,
		TasksPerNode: 1,
		Wrap:         wrap,
	})
	if err != nil {
		return err
	}
	s.job = job
	s.containerNamePrefix = fmt.Sprintf("e2e-docker-%s-", job.ID)
	s.exec.Logf("Docker containers: selected workers=%s job_id=%s stdout=%s stderr=%s",
		strings.Join(s.workers, ","), job.ID, job.StdoutPath, job.StderrPath)
	return nil
}

func (s *DockerContainers) theDockerContainerJobIsRunning(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("Docker job ID is empty")
	}
	return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, s.job,
		s.slurm.WaitForJobRunning(ctx, s.job.ID, dockerJobStartTimeout))
}

func (s *DockerContainers) dockerImageAndRuntimeStorageIsPopulatedOnAWorker(ctx context.Context) error {
	if err := framework.WaitForTreeEntriesOnWorker(ctx, s.exec, s.connectionWorker, "/mnt/image-storage/docker/overlay2/", "Docker overlay2 storage", dockerProbeTimeout); err != nil {
		return err
	}
	return framework.WaitForTreeEntriesOnWorker(ctx, s.exec, s.connectionWorker, "/mnt/image-storage/docker/containerd/daemon/io.containerd.content.v1.content/blobs/sha256/", "Docker container content blobs", dockerProbeTimeout)
}

func (s *DockerContainers) dockerContainersFromTheJobAreRunningOnSelectedWorkers(ctx context.Context) error {
	err := framework.WaitForWithJobAlive(ctx, s.exec, s.slurm, s.job, "Docker containers running on selected workers",
		dockerProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			for _, worker := range s.workers {
				currentIDs, err := s.dockerContainerIDsByNamePrefix(waitCtx, worker)
				if err != nil {
					return false, err
				}
				if len(currentIDs) == 0 {
					return false, nil
				}
			}
			return true, nil
		})
	return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, s.job, err)
}

func (s *DockerContainers) theDockerContainerJobIsCancelled(ctx context.Context) error {
	return s.requestCurrentJobCancellation(ctx)
}

func (s *DockerContainers) dockerContainersFromTheJobAreStoppedExplicitly(ctx context.Context) error {
	running, err := s.trackedDockerContainerCount(ctx)
	if err != nil {
		return err
	}
	if running == 0 {
		s.exec.Logf("Docker containers: no matching containers found after scancel; explicit stop was a no-op")
		return nil
	}

	s.exec.Logf("Docker containers: %d matching container(s) still running after scancel; stopping explicitly", running)
	s.stopContainersByNamePrefix(ctx)
	return nil
}

func (s *DockerContainers) dockerContainersFromTheJobAreNoLongerRunning(ctx context.Context) error {
	if err := s.waitForTrackedContainersGone(ctx, dockerContainerStopTimeout); err != nil {
		return err
	}
	return s.waitForCurrentJobGone(ctx)
}

func (s *DockerContainers) aDockerGPUSmokeJobIsSubmittedOnOneGPUWorker(ctx context.Context) error {
	// Future option: omit --nodelist and discover the allocated worker after
	// submission, letting Slurm avoid busy nodes before Docker log collection.
	workers, err := s.slurm.AnyGPUWorkers(1)
	if err != nil {
		return err
	}
	s.workers = workers
	s.connectionWorker = workers[0]

	wrap := fmt.Sprintf("srun docker run --name e2e-docker-gpu-${SLURM_JOB_ID}-${SLURM_NODEID} --gpus=all -e NVIDIA_DRIVER_CAPABILITIES=utility %s nvidia-smi -L",
		framework.ShellQuote(dockerGPUImage),
	)
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-docker-gpu-smoke",
		Nodes:        1,
		Nodelist:     s.workers,
		GPUsPerNode:  1,
		TasksPerNode: 1,
		Wrap:         wrap,
	})
	if err != nil {
		return err
	}
	s.job = job
	s.containerNamePrefix = fmt.Sprintf("e2e-docker-gpu-%s-", job.ID)
	s.exec.Logf("Docker GPU smoke: selected worker=%s job_id=%s stdout=%s stderr=%s",
		s.connectionWorker, job.ID, job.StdoutPath, job.StderrPath)
	return nil
}

func (s *DockerContainers) theDockerGPUSmokeJobSucceedsAndReportsVisibleGPUs(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("Docker GPU smoke job ID is empty")
	}
	job := s.job
	// A sacct-only wait would let us read Docker logs earlier, but it can hide
	// Slurm cleanup time and transfer resource-release waits to later scenarios.
	if err := waitForJobSucceeded(ctx, s.exec, s.slurm, job, dockerGPUSmokeTimeout); err != nil {
		return err
	}
	logs, err := s.dockerContainerLogsByNamePrefix(ctx, s.connectionWorker)
	if err != nil {
		return err
	}
	if err := assertGPUListing(logs, fmt.Sprintf("Docker container logs on %s", s.connectionWorker)); err != nil {
		return err
	}
	s.removeContainersByNamePrefix(ctx)
	s.job = framework.SbatchJob{}
	return nil
}

func (s *DockerContainers) waitForTrackedContainersGone(ctx context.Context, timeout time.Duration) error {
	if len(s.workers) == 0 {
		return fmt.Errorf("Docker workers are not selected")
	}
	return s.exec.WaitFor(ctx, "Docker containers stopped on selected workers", timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		for _, worker := range s.workers {
			currentIDs, err := s.dockerContainerIDsByNamePrefix(waitCtx, worker)
			if err != nil {
				return false, err
			}
			if len(currentIDs) > 0 {
				return false, nil
			}
		}
		return true, nil
	})
}

func (s *DockerContainers) requestCurrentJobCancellation(ctx context.Context) error {
	if s.job.IsZero() {
		return nil
	}

	if err := s.slurm.CancelJob(ctx, s.job.ID, 0); err != nil {
		return fmt.Errorf("cancel Docker job %s: %w", s.job.ID, err)
	}
	return nil
}

func (s *DockerContainers) waitForCurrentJobGone(ctx context.Context) error {
	if s.job.IsZero() {
		return nil
	}

	if err := s.slurm.WaitForJobGone(ctx, s.job.ID, dockerJobCancelTimeout); err != nil {
		return fmt.Errorf("wait for Docker job %s to finish: %w", s.job.ID, err)
	}
	s.job = framework.SbatchJob{}
	return nil
}

func (s *DockerContainers) trackedDockerContainerCount(ctx context.Context) (int, error) {
	if len(s.workers) == 0 {
		return 0, fmt.Errorf("Docker workers are not selected")
	}

	count := 0
	for _, worker := range s.workers {
		currentIDs, err := s.dockerContainerIDsByNamePrefix(ctx, worker)
		if err != nil {
			return 0, err
		}
		count += len(currentIDs)
	}
	return count, nil
}

func (s *DockerContainers) dockerContainerIDsByNamePrefix(ctx context.Context, worker string) (map[string]struct{}, error) {
	if s.containerNamePrefix == "" {
		return nil, fmt.Errorf("Docker container name prefix is empty")
	}

	out, err := s.exec.Worker(worker).RunWithDefaultRetry(ctx,
		fmt.Sprintf("sudo docker ps --filter name=%s --format '{{.ID}}'", framework.ShellQuote(s.containerNamePrefix)))
	if err != nil {
		return nil, err
	}
	return parseIDSet(out), nil
}

func (s *DockerContainers) dockerContainerIDsByNamePrefixAll(ctx context.Context, worker string) (map[string]struct{}, error) {
	if s.containerNamePrefix == "" {
		return nil, fmt.Errorf("Docker container name prefix is empty")
	}

	out, err := s.exec.Worker(worker).RunWithDefaultRetry(ctx,
		fmt.Sprintf("sudo docker ps -a --filter name=%s --format '{{.ID}}'", framework.ShellQuote(s.containerNamePrefix)))
	if err != nil {
		return nil, err
	}
	return parseIDSet(out), nil
}

func (s *DockerContainers) dockerContainerLogsByNamePrefix(ctx context.Context, worker string) (string, error) {
	ids, err := s.dockerContainerIDsByNamePrefixAll(ctx, worker)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("no Docker containers found with prefix %s on worker %s", s.containerNamePrefix, worker)
	}

	logs := make([]string, 0, len(ids))
	for id := range ids {
		out, err := s.exec.Worker(worker).RunWithDefaultRetry(ctx,
			fmt.Sprintf("sudo docker logs %s 2>&1", framework.ShellQuote(id)))
		if err != nil {
			return "", err
		}
		logs = append(logs, out)
	}
	return strings.Join(logs, "\n"), nil
}

func parseIDSet(output string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		id := strings.TrimSpace(line)
		if id == "" {
			continue
		}
		result[id] = struct{}{}
	}
	return result
}

func (s *DockerContainers) stopContainersByNamePrefix(ctx context.Context) {
	if s.containerNamePrefix == "" {
		return
	}

	for _, worker := range s.workers {
		out, err := s.exec.Worker(worker).RunWithDefaultRetry(ctx,
			fmt.Sprintf("sudo docker ps --filter name=%s --format '{{.ID}}'", framework.ShellQuote(s.containerNamePrefix)))
		if err != nil {
			s.exec.Logf("Docker cleanup: list containers on worker %s failed: %v", worker, err)
			continue
		}
		for id := range parseIDSet(out) {
			if _, err := s.exec.Worker(worker).Run(ctx, fmt.Sprintf("sudo docker stop %s >/dev/null 2>&1 || true", framework.ShellQuote(id))); err != nil {
				s.exec.Logf("Docker cleanup: stop container %s on worker %s failed: %v", id, worker, err)
			}
		}
	}
}

func (s *DockerContainers) removeContainersByNamePrefix(ctx context.Context) {
	if s.containerNamePrefix == "" {
		return
	}

	for _, worker := range s.workers {
		ids, err := s.dockerContainerIDsByNamePrefixAll(ctx, worker)
		if err != nil {
			s.exec.Logf("Docker cleanup: list all containers on worker %s failed: %v", worker, err)
			continue
		}
		for id := range ids {
			if _, err := s.exec.Worker(worker).Run(ctx, fmt.Sprintf("sudo docker rm -f %s >/dev/null 2>&1 || true", framework.ShellQuote(id))); err != nil {
				s.exec.Logf("Docker cleanup: remove container %s on worker %s failed: %v", id, worker, err)
			}
		}
	}
}
