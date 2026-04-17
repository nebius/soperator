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

	dockerImage = "cr.eu-north1.nebius.cloud/soperator/active_checks:12.9.0-ubuntu24.04-nccl_tests2.16.4-3935b93"
	dockerARP   = "NCCL_P2P_DISABLE=1 NCCL_SHM_DISABLE=1 NCCL_ALGO=Ring all_reduce_perf -b 8G -e 8G -f 2 -g 8 -N 0"

	dockerJobStartTimeout = 20 * time.Minute
	dockerProbeTimeout    = 10 * time.Minute
	dockerStopTimeout     = 5 * time.Minute
)

type DockerContainers struct {
	exec    framework.Exec
	workers []string
	jobID   string

	containerNamePrefix string
}

func NewDockerContainers(exec framework.Exec) *DockerContainers {
	return &DockerContainers{exec: exec}
}

func (s *DockerContainers) Register(sc *godog.ScenarioContext) {
	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		if path.Base(scenario.Uri) != dockerContainersFeatureFile {
			return ctx, nil
		}

		if s.jobID != "" {
			if cleanupErr := s.cancelJob(context.Background()); cleanupErr != nil {
				s.exec.Logf("cleanup: cancel docker job: %v", cleanupErr)
			}
		}
		if cleanupErr := s.stopContainersByNamePrefix(context.Background()); cleanupErr != nil {
			s.exec.Logf("cleanup: stop docker containers by prefix: %v", cleanupErr)
		}
		return ctx, nil
	})

	sc.Step(`^a long-running Docker NCCL job is submitted on two GPU workers$`, s.aLongRunningDockerNCCLJobIsSubmittedOnTwoGPUWorkers)
	sc.Step(`^the Docker NCCL job is running$`, s.theDockerNCCLJobIsRunning)
	sc.Step(`^Docker overlayfs storage is populated on a worker$`, s.dockerOverlayfsStorageIsPopulatedOnAWorker)
	sc.Step(`^Docker container content blobs are populated on a worker$`, s.dockerContainerContentBlobsArePopulatedOnAWorker)
	sc.Step(`^a Docker container from the job is running on workers$`, s.aDockerContainerFromTheJobIsRunningOnWorkers)
	sc.Step(`^the Docker NCCL job is cancelled$`, s.theDockerNCCLJobIsCancelled)
	sc.Step(`^Docker containers from that job are no longer running$`, s.dockerContainersFromThatJobAreNoLongerRunning)
}

func (s *DockerContainers) aLongRunningDockerNCCLJobIsSubmittedOnTwoGPUWorkers(ctx context.Context) error {
	workers, err := selectGPUWorkers(ctx, s.exec, 2)
	if err != nil {
		return err
	}
	s.workers = workers

	nodelist := strings.Join(workers, ",")
	wrap := fmt.Sprintf("srun docker run --rm --name e2e-docker-${SLURM_JOB_ID}-${SLURM_NODEID} --gpus=all --device=/dev/infiniband %s bash -lc %s",
		framework.ShellQuote(dockerImage),
		framework.ShellQuote(dockerARP),
	)
	submit := fmt.Sprintf(
		"sbatch --parsable -N 2 --nodelist=%s --gpus-per-node=8 --job-name=e2e-docker-containers --wrap=%s",
		framework.ShellQuote(nodelist),
		framework.ShellQuote(wrap),
	)

	out, err := s.exec.ExecJail(ctx, submit)
	if err != nil {
		return fmt.Errorf("submit Docker NCCL job: %w", err)
	}
	jobID, err := parseSbatchJobID(out)
	if err != nil {
		return fmt.Errorf("parse Docker job id: %w", err)
	}
	s.jobID = jobID
	s.containerNamePrefix = fmt.Sprintf("e2e-docker-%s-", jobID)
	s.exec.Logf("docker containers: selected workers=%s job_id=%s", nodelist, jobID)
	return nil
}

func (s *DockerContainers) theDockerNCCLJobIsRunning(ctx context.Context) error {
	if s.jobID == "" {
		return fmt.Errorf("docker job id is empty")
	}
	return waitForJobRunning(ctx, s.exec, s.jobID, dockerJobStartTimeout)
}

func (s *DockerContainers) dockerOverlayfsStorageIsPopulatedOnAWorker(ctx context.Context) error {
	return s.waitForTreeEntriesOnWorker(ctx, "/mnt/image-storage/docker/rootfs/overlayfs/", "docker overlayfs storage")
}

func (s *DockerContainers) dockerContainerContentBlobsArePopulatedOnAWorker(ctx context.Context) error {
	return s.waitForTreeEntriesOnWorker(ctx, "/mnt/image-storage/docker/containerd/daemon/io.containerd.content.v1.content/blobs/sha256/", "docker container content blobs")
}

func (s *DockerContainers) aDockerContainerFromTheJobIsRunningOnWorkers(ctx context.Context) error {
	return s.exec.WaitFor(ctx, "docker containers running on selected workers", dockerProbeTimeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
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
}

func (s *DockerContainers) theDockerNCCLJobIsCancelled(ctx context.Context) error {
	if err := s.cancelJob(ctx); err != nil {
		return err
	}
	s.jobID = ""
	return nil
}

func (s *DockerContainers) dockerContainersFromThatJobAreNoLongerRunning(ctx context.Context) error {
	return s.waitForTrackedContainersGone(ctx, dockerStopTimeout)
}

func (s *DockerContainers) waitForTrackedContainersGone(ctx context.Context, timeout time.Duration) error {
	return s.exec.WaitFor(ctx, "docker containers stopped on selected workers", timeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
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

func (s *DockerContainers) waitForTreeEntriesOnWorker(ctx context.Context, storagePath, description string) error {
	if len(s.workers) == 0 {
		return fmt.Errorf("docker workers are not selected")
	}
	worker := s.workers[0]

	return s.exec.WaitFor(ctx, description, dockerProbeTimeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
		out, err := runWorkerCommandWithDefaultRetry(waitCtx, s.exec, worker, fmt.Sprintf("sudo tree -L 2 -a %s", framework.ShellQuote(storagePath)))
		if err != nil {
			return false, err
		}
		return treeOutputHasEntries(out), nil
	})
}

func (s *DockerContainers) cancelJob(ctx context.Context) error {
	if s.jobID == "" {
		return nil
	}

	if err := cancelSlurmJob(ctx, s.exec, s.jobID, dockerStopTimeout); err != nil {
		return fmt.Errorf("cancel Docker job %s: %w", s.jobID, err)
	}
	return nil
}

func (s *DockerContainers) dockerContainerIDsByNamePrefix(ctx context.Context, worker string) (map[string]struct{}, error) {
	if s.containerNamePrefix == "" {
		return nil, fmt.Errorf("docker container name prefix is empty")
	}

	out, err := runWorkerCommandWithDefaultRetry(ctx, s.exec, worker,
		fmt.Sprintf("sudo docker ps --filter name=%s --format '{{.ID}}'", framework.ShellQuote(s.containerNamePrefix)))
	if err != nil {
		return nil, err
	}
	return parseIDSet(out), nil
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

func (s *DockerContainers) stopContainersByNamePrefix(ctx context.Context) error {
	if s.containerNamePrefix == "" {
		return nil
	}

	for _, worker := range s.workers {
		out, err := runWorkerCommandWithDefaultRetry(ctx, s.exec, worker,
			fmt.Sprintf("sudo docker ps --filter name=%s --format '{{.ID}}'", framework.ShellQuote(s.containerNamePrefix)))
		if err != nil {
			continue
		}
		for id := range parseIDSet(out) {
			_, _ = runWorkerCommand(ctx, s.exec, worker, fmt.Sprintf("sudo docker stop %s >/dev/null 2>&1 || true", framework.ShellQuote(id)))
		}
	}
	return nil
}
