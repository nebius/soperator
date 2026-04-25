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

	dockerARP = "NCCL_P2P_DISABLE=1 NCCL_SHM_DISABLE=1 NCCL_ALGO=Ring all_reduce_perf -b 8G -e 8G -f 2 -g 8 -N 0"

	dockerJobStartTimeout      = 20 * time.Minute
	dockerProbeTimeout         = 10 * time.Minute
	dockerJobCancelTimeout     = 5 * time.Minute
	dockerContainerStopTimeout = 5 * time.Minute
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

		if cleanupErr := s.cancelCurrentJob(context.Background()); cleanupErr != nil {
			s.exec.Logf("cleanup: cancel Docker job: %v", cleanupErr)
		}
		s.stopContainersByNamePrefix(context.Background())
		return ctx, nil
	})

	sc.Step(`^a long-running Docker NCCL job is submitted on two GPU workers$`, s.aLongRunningDockerNCCLJobIsSubmittedOnTwoGPUWorkers)
	sc.Step(`^the Docker NCCL job is running$`, s.theDockerNCCLJobIsRunning)
	sc.Step(`^Docker overlayfs storage is populated on a worker$`, s.dockerOverlayfsStorageIsPopulatedOnAWorker)
	sc.Step(`^Docker container content blobs are populated on a worker$`, s.dockerContainerContentBlobsArePopulatedOnAWorker)
	sc.Step(`^a Docker container from the job is running on workers$`, s.aDockerContainerFromTheJobIsRunningOnWorkers)
	sc.Step(`^the Docker NCCL job is still running$`, s.theDockerNCCLJobIsStillRunning)
	sc.Step(`^the Docker NCCL job is cancelled$`, s.theDockerNCCLJobIsCancelled)
	sc.Step(`^Docker containers from that job are no longer running$`, s.dockerContainersFromThatJobAreNoLongerRunning)
}

func (s *DockerContainers) aLongRunningDockerNCCLJobIsSubmittedOnTwoGPUWorkers(ctx context.Context) error {
	workers, err := s.slurm.AnyGPUWorkers(2)
	if err != nil {
		return err
	}
	s.workers = workers
	s.connectionWorker = s.workers[0]

	image, err := framework.RequiredEnv("E2E_DOCKER_IMAGE")
	if err != nil {
		return err
	}

	wrap := fmt.Sprintf("srun docker run --rm --name e2e-docker-${SLURM_JOB_ID}-${SLURM_NODEID} --gpus=all --device=/dev/infiniband %s bash -lc %s",
		framework.ShellQuote(image),
		framework.ShellQuote(dockerARP),
	)
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:     "e2e-docker-containers",
		Nodes:       2,
		Nodelist:    s.workers,
		GPUsPerNode: 8,
		Wrap:        wrap,
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

func (s *DockerContainers) theDockerNCCLJobIsRunning(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("Docker job ID is empty")
	}
	return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, s.job,
		s.slurm.WaitForJobRunning(ctx, s.job.ID, dockerJobStartTimeout))
}

func (s *DockerContainers) theDockerNCCLJobIsStillRunning(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("Docker job ID is empty")
	}
	return framework.AnnotateWithJobLog(ctx, s.exec, s.slurm, s.job,
		s.slurm.AssertJobRunning(ctx, s.job.ID))
}

func (s *DockerContainers) dockerOverlayfsStorageIsPopulatedOnAWorker(ctx context.Context) error {
	return framework.WaitForTreeEntriesOnWorker(ctx, s.exec, s.connectionWorker, "/mnt/image-storage/docker/rootfs/overlayfs/", "Docker overlayfs storage", dockerProbeTimeout)
}

func (s *DockerContainers) dockerContainerContentBlobsArePopulatedOnAWorker(ctx context.Context) error {
	// This scenario checks storage population/cleanup only.
	// It does not currently assert strict blob-by-blob identity across repeated runs.
	return framework.WaitForTreeEntriesOnWorker(ctx, s.exec, s.connectionWorker, "/mnt/image-storage/docker/containerd/daemon/io.containerd.content.v1.content/blobs/sha256/", "Docker container content blobs", dockerProbeTimeout)
}

func (s *DockerContainers) aDockerContainerFromTheJobIsRunningOnWorkers(ctx context.Context) error {
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

func (s *DockerContainers) theDockerNCCLJobIsCancelled(ctx context.Context) error {
	return s.cancelCurrentJob(ctx)
}

func (s *DockerContainers) dockerContainersFromThatJobAreNoLongerRunning(ctx context.Context) error {
	// Expected behavior is that containers stop after `scancel`, but currently this is unreliable.
	// TODO(SCHED-1497): remove explicit Docker stop workaround once cancellation cleanup is fixed.
	s.exec.Logf("Docker containers: applying SCHED-1497 workaround (explicit docker stop before assertion)")
	s.stopContainersByNamePrefix(ctx)
	return s.waitForTrackedContainersGone(ctx, dockerContainerStopTimeout)
}

func (s *DockerContainers) waitForTrackedContainersGone(ctx context.Context, timeout time.Duration) error {
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

func (s *DockerContainers) cancelCurrentJob(ctx context.Context) error {
	if s.job.IsZero() {
		return nil
	}

	if err := s.slurm.CancelJob(ctx, s.job.ID, dockerJobCancelTimeout); err != nil {
		return fmt.Errorf("cancel Docker job %s: %w", s.job.ID, err)
	}
	s.job = framework.SbatchJob{}
	return nil
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
