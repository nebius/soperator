package sharedsteps

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	// Small mirrored image; Docker lifecycle only needs sh/sleep and storage population.
	dockerLifecycleImage   = "cr.eu-north1.nebius.cloud/soperator/busybox"
	dockerLifecycleCommand = "echo ready; sleep 3600"

	dockerLocalStorageRoot = "/mnt/image-storage/docker"

	dockerJobStartTimeout      = 5 * time.Minute
	dockerProbeTimeout         = 5 * time.Minute
	dockerJobCancelTimeout     = 3 * time.Minute
	dockerContainerStopTimeout = 3 * time.Minute
	dockerGPUSmokeTimeout      = 10 * time.Minute
	dockerSSHSmokeTimeout      = 10 * time.Minute

	dockerSSHUserName   = "dockeruser"
	dockerSSHKeyName    = "soperator_e2e_docker_ssh"
	dockerSSHKeyComment = "soperator-e2e-docker-ssh"
)

type DockerContainers struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector
	workers  []framework.WorkerInfo
	job      framework.SbatchJob

	containerNamePrefix string
	connectionWorker    framework.WorkerInfo

	loginSSHContainerName string
	loginSSHIdentitySet   bool
	loginSSHOutput        string
}

func NewDockerContainers(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient,
	selector *framework.WorkerSelector,
) *DockerContainers {
	return &DockerContainers{
		info:     info,
		runtime:  runtime,
		slurm:    slurm,
		kubectl:  kubectl,
		selector: selector,
	}
}

func (s *DockerContainers) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^login Docker is enabled$`, s.loginDockerIsEnabled)
	sc.Step(`^a Docker SSH test user exists$`, s.aDockerSSHTestUserExists)
	sc.Step(`^the user runs Docker lifecycle commands over SSH on the login node$`, s.theUserRunsDockerLifecycleCommandsOverSSHOnTheLoginNode)
	sc.Step(`^Docker uses the login proxy, image storage, and the user's cgroup$`, s.dockerUsesTheLoginProxyImageStorageAndTheUsersCgroup)
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

func (s *DockerContainers) CleanupAndReset(ctx context.Context) {
	if s.loginSSHContainerName != "" && s.loginSSHIdentitySet {
		cleanupCommand := fmt.Sprintf(
			"docker rm -f %s >/dev/null 2>&1 || true",
			framework.ShellQuote(s.loginSSHContainerName),
		)
		if _, cleanupErr := s.runLoginDockerSSHCommand(ctx, cleanupCommand); cleanupErr != nil {
			s.runtime.Logf("cleanup: remove login Docker SSH container: %v", cleanupErr)
		}
	}
	if s.loginSSHIdentitySet {
		if cleanupErr := removeSSHTestIdentity(
			ctx,
			s.runtime,
			dockerSSHUserName,
			dockerSSHKeyName,
			dockerSSHKeyComment,
		); cleanupErr != nil {
			s.runtime.Logf("cleanup: remove login Docker SSH identity: %v", cleanupErr)
		}
	}
	if cleanupErr := s.requestCurrentJobCancellation(ctx); cleanupErr != nil {
		s.runtime.Logf("cleanup: cancel Docker job: %v", cleanupErr)
	}
	s.stopContainersByNamePrefix(ctx)
	s.removeContainersByNamePrefix(ctx)
	if cleanupErr := s.waitForCurrentJobGone(ctx); cleanupErr != nil {
		s.runtime.Logf("cleanup: wait for Docker job to finish: %v", cleanupErr)
	}
	s.workers = nil
	s.job = framework.SbatchJob{}
	s.containerNamePrefix = ""
	s.connectionWorker = framework.WorkerInfo{}
	s.loginSSHContainerName = ""
	s.loginSSHIdentitySet = false
	s.loginSSHOutput = ""
}

func (s *DockerContainers) loginDockerIsEnabled(ctx context.Context) error {
	var cluster kubeobjects.SlurmCluster
	if err := s.kubectl.GetJSON(
		ctx,
		&cluster,
		"get",
		"slurmcluster",
		s.info.SlurmClusterName,
		"-n",
		framework.SoperatorNamespace,
		"-o",
		"json",
	); err != nil {
		return fmt.Errorf("get SlurmCluster for login Docker validation: %w", err)
	}
	if !cluster.Spec.SlurmNodes.Login.Docker.Enabled {
		s.runtime.Logf("login Docker is disabled, skipping scenario")
		return godog.ErrSkip
	}

	return nil
}

func (s *DockerContainers) aDockerSSHTestUserExists(ctx context.Context) error {
	if err := ensureSSHTestUser(ctx, s.runtime, dockerSSHUserName); err != nil {
		return err
	}

	s.loginSSHIdentitySet = true
	return ensureSSHTestIdentity(
		ctx,
		s.runtime,
		dockerSSHUserName,
		dockerSSHKeyName,
		dockerSSHKeyComment,
	)
}

func (s *DockerContainers) theUserRunsDockerLifecycleCommandsOverSSHOnTheLoginNode(ctx context.Context) error {
	s.loginSSHContainerName = fmt.Sprintf("soperator-e2e-login-docker-%d", time.Now().UnixNano())
	remoteCommand := fmt.Sprintf(`
set -euo pipefail
name=%s
image=%s
build_image="${name}-build:latest"
build_context=$(mktemp -d)
cleanup() {
    docker rm -f "${name}" >/dev/null 2>&1 || true
    docker image rm -f "${build_image}" >/dev/null 2>&1 || true
    rm -rf "${build_context}"
}
trap cleanup EXIT

echo "DOCKER_HOST=${DOCKER_HOST:-}"
echo "DOCKER_ROOT=$(docker info --format '{{.DockerRootDir}}')"
echo "DOCKER_CGROUP_DRIVER=$(docker info --format '{{.CgroupDriver}}')"
if docker -H unix:///run/soperator-dockerd.sock version >/dev/null 2>&1; then
    echo "PRIVATE_SOCKET_BLOCKED=false"
else
    echo "PRIVATE_SOCKET_BLOCKED=true"
fi

printf '%%s\n' \
    "FROM ${image}" \
    'ARG CACHE_BUST' \
    'RUN test -n "$CACHE_BUST" && echo BUILD_OK > /build-result' \
    > "${build_context}/Dockerfile"
docker build \
    --no-cache \
    --build-arg "CACHE_BUST=${name}" \
    --tag "${build_image}" \
    "${build_context}" >/dev/null
docker run --rm "${build_image}" grep -qx BUILD_OK /build-result
echo "BUILD_OK=true"

docker run -d --name "${name}" "${image}" sleep 300 >/dev/null
container_pid=$(docker inspect --format '{{.State.Pid}}' "${name}")
container_cgroup=$(sed -n 's/^0:://p' "/proc/${container_pid}/cgroup")
echo "EXPECTED_USER_CGROUP=/users/user-$(id -u)/docker/"
echo "CONTAINER_CGROUP=${container_cgroup}"

docker exec "${name}" sh -c 'echo EXEC_OK'
docker run --rm "${image}" sh -c 'echo ATTACHED_RUN_OK'
`, framework.ShellQuote(s.loginSSHContainerName), framework.ShellQuote(dockerLifecycleImage))

	output, err := s.runLoginDockerSSHCommand(ctx, remoteCommand)
	if err != nil {
		return fmt.Errorf("run Docker lifecycle over login SSH: %w", err)
	}
	s.loginSSHOutput = output
	return nil
}

func (s *DockerContainers) dockerUsesTheLoginProxyImageStorageAndTheUsersCgroup() error {
	checks := []struct {
		key  string
		want string
	}{
		{key: "DOCKER_HOST", want: ""},
		{key: "DOCKER_ROOT", want: dockerLocalStorageRoot},
		{key: "DOCKER_CGROUP_DRIVER", want: "cgroupfs"},
		{key: "PRIVATE_SOCKET_BLOCKED", want: "true"},
	}
	for _, check := range checks {
		if got := dockerSSHOutputValue(s.loginSSHOutput, check.key); got != check.want {
			return fmt.Errorf(
				"login Docker %s = %q, want %q; output: %s",
				check.key,
				got,
				check.want,
				strings.TrimSpace(s.loginSSHOutput),
			)
		}
	}

	expectedCgroup := dockerSSHOutputValue(s.loginSSHOutput, "EXPECTED_USER_CGROUP")
	containerCgroup := dockerSSHOutputValue(s.loginSSHOutput, "CONTAINER_CGROUP")
	if expectedCgroup == "" || !strings.Contains(containerCgroup, expectedCgroup) {
		return fmt.Errorf(
			"login Docker container cgroup %q does not contain per-user parent %q; output: %s",
			containerCgroup,
			expectedCgroup,
			strings.TrimSpace(s.loginSSHOutput),
		)
	}
	for _, marker := range []string{"BUILD_OK=true", "EXEC_OK", "ATTACHED_RUN_OK"} {
		if !strings.Contains(s.loginSSHOutput, marker) {
			return fmt.Errorf("login Docker output does not contain %q: %s", marker, strings.TrimSpace(s.loginSSHOutput))
		}
	}

	return nil
}

func (s *DockerContainers) runLoginDockerSSHCommand(ctx context.Context, remoteCommand string) (string, error) {
	return runSSHCommand(
		ctx,
		s.runtime,
		dockerSSHUserName,
		dockerSSHKeyName,
		"localhost",
		dockerSSHSmokeTimeout,
		remoteCommand,
	)
}

func dockerSSHOutputValue(output, key string) string {
	prefix := key + "="
	for line := range strings.SplitSeq(output, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			return value
		}
	}
	return ""
}

func (s *DockerContainers) aLongRunningDockerContainerJobIsSubmittedOnTwoWorkers(ctx context.Context) error {
	workers, err := s.selector.PickWorkers(ctx, 2)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
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
		Nodelist:     framework.WorkerNames(s.workers),
		TasksPerNode: 1,
		Wrap:         wrap,
	})
	if err != nil {
		return err
	}
	s.job = job
	s.containerNamePrefix = fmt.Sprintf("e2e-docker-%s-", job.ID)
	s.runtime.Logf("Docker containers: selected workers=%s job_id=%s stdout=%s stderr=%s",
		strings.Join(framework.WorkerNames(s.workers), ","), job.ID, job.StdoutPath, job.StderrPath)
	return nil
}

func (s *DockerContainers) theDockerContainerJobIsRunning(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("Docker job ID is empty")
	}
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job,
		s.slurm.WaitForJobRunning(ctx, s.job.ID, dockerJobStartTimeout))
}

func (s *DockerContainers) dockerImageAndRuntimeStorageIsPopulatedOnAWorker(ctx context.Context) error {
	if s.connectionWorker.Name == "" {
		return fmt.Errorf("Docker connection worker is not selected")
	}

	err := framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, s.job, "Docker image and runtime storage on local disk",
		dockerProbeTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			rootDir, err := s.dockerRootDir(waitCtx, s.connectionWorker)
			if err != nil {
				return false, err
			}
			if !pathIsUnder(rootDir, dockerLocalStorageRoot) {
				return false, fmt.Errorf("expected Docker root dir under %s, got %q", dockerLocalStorageRoot, rootDir)
			}

			imageID, err := s.dockerImageID(waitCtx, s.connectionWorker, dockerLifecycleImage)
			if err != nil {
				return false, err
			}
			if imageID == "" {
				return false, nil
			}

			containerIDs, err := s.dockerContainerIDsByNamePrefix(waitCtx, s.connectionWorker)
			if err != nil {
				return false, err
			}
			for containerID := range containerIDs {
				paths, err := s.dockerContainerGraphDriverPaths(waitCtx, s.connectionWorker, containerID)
				if err != nil {
					return false, err
				}
				if paths == "" {
					return false, nil
				}
				if !graphDriverPathsUnder(paths, dockerLocalStorageRoot) {
					return false, fmt.Errorf("expected Docker graph-driver paths under %s, got:\n%s", dockerLocalStorageRoot, paths)
				}

				cgroupParent, err := s.dockerContainerCgroupParent(waitCtx, s.connectionWorker, containerID)
				if err != nil {
					return false, err
				}
				if !dockerCgroupParentBelongsToJob(cgroupParent, s.job.ID) {
					return false, fmt.Errorf(
						"expected Docker cgroup parent for Slurm job %s, got %q",
						s.job.ID,
						cgroupParent,
					)
				}
				return true, nil
			}
			return false, nil
		})
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job, err)
}

func (s *DockerContainers) dockerContainersFromTheJobAreRunningOnSelectedWorkers(ctx context.Context) error {
	err := framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, s.job, "Docker containers running on selected workers",
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
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job, err)
}

func (s *DockerContainers) theDockerContainerJobIsCancelled(ctx context.Context) error {
	return s.requestCurrentJobCancellation(ctx)
}

func (s *DockerContainers) dockerContainersFromTheJobAreStoppedExplicitly(ctx context.Context) error {
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
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.workers = workers
	s.connectionWorker = workers[0]

	wrap := fmt.Sprintf("srun docker run --name e2e-docker-gpu-${SLURM_JOB_ID}-${SLURM_NODEID} --gpus=all -e NVIDIA_DRIVER_CAPABILITIES=utility %s nvidia-smi -L",
		framework.ShellQuote(gpuSmokeDockerImage),
	)
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-docker-gpu-smoke",
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
	s.containerNamePrefix = fmt.Sprintf("e2e-docker-gpu-%s-", job.ID)
	s.runtime.Logf("Docker GPU smoke: selected worker=%s job_id=%s stdout=%s stderr=%s",
		s.connectionWorker.Name, job.ID, job.StdoutPath, job.StderrPath)
	return nil
}

func (s *DockerContainers) theDockerGPUSmokeJobSucceedsAndReportsVisibleGPUs(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("Docker GPU smoke job ID is empty")
	}
	job := s.job
	// A sacct-only wait would let us read Docker logs earlier, but it can hide
	// Slurm cleanup time and transfer resource-release waits to later scenarios.
	if err := waitForJobSucceeded(ctx, s.runtime, s.slurm, job, dockerGPUSmokeTimeout); err != nil {
		return err
	}
	logs, err := s.dockerContainerLogsByNamePrefix(ctx, s.connectionWorker)
	if err != nil {
		return err
	}
	if err := assertGPUListing(logs, fmt.Sprintf("Docker container logs on %s", s.connectionWorker.Name)); err != nil {
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
	return s.runtime.WaitFor(ctx, "Docker containers stopped on selected workers", timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
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
	return nil
}

func (s *DockerContainers) dockerContainerIDsByNamePrefix(ctx context.Context, worker framework.WorkerInfo) (map[string]struct{}, error) {
	if s.containerNamePrefix == "" {
		return nil, fmt.Errorf("Docker container name prefix is empty")
	}

	out, err := s.runtime.Worker(worker).RunWithDefaultRetry(ctx,
		fmt.Sprintf("sudo docker ps --filter name=%s --format '{{.ID}}'", framework.ShellQuote(s.containerNamePrefix)))
	if err != nil {
		return nil, err
	}
	return parseIDSet(out), nil
}

func (s *DockerContainers) dockerRootDir(ctx context.Context, worker framework.WorkerInfo) (string, error) {
	out, err := s.runtime.Worker(worker).RunWithDefaultRetry(ctx, "sudo docker info --format '{{.DockerRootDir}}'")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (s *DockerContainers) dockerImageID(ctx context.Context, worker framework.WorkerInfo, image string) (string, error) {
	out, err := s.runtime.Worker(worker).RunWithDefaultRetry(ctx,
		fmt.Sprintf("sudo docker image inspect --format '{{.Id}}' %s", framework.ShellQuote(image)))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (s *DockerContainers) dockerContainerGraphDriverPaths(ctx context.Context, worker framework.WorkerInfo, containerID string) (string, error) {
	out, err := s.runtime.Worker(worker).RunWithDefaultRetry(ctx,
		fmt.Sprintf("sudo docker inspect --format '{{range $key, $value := .GraphDriver.Data}}{{println $value}}{{end}}' %s", framework.ShellQuote(containerID)))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (s *DockerContainers) dockerContainerCgroupParent(ctx context.Context, worker framework.WorkerInfo, containerID string) (string, error) {
	out, err := s.runtime.Worker(worker).RunWithDefaultRetry(ctx,
		fmt.Sprintf("sudo docker inspect --format '{{.HostConfig.CgroupParent}}' %s", framework.ShellQuote(containerID)))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (s *DockerContainers) dockerContainerIDsByNamePrefixAll(ctx context.Context, worker framework.WorkerInfo) (map[string]struct{}, error) {
	if s.containerNamePrefix == "" {
		return nil, fmt.Errorf("Docker container name prefix is empty")
	}

	out, err := s.runtime.Worker(worker).RunWithDefaultRetry(ctx,
		fmt.Sprintf("sudo docker ps -a --filter name=%s --format '{{.ID}}'", framework.ShellQuote(s.containerNamePrefix)))
	if err != nil {
		return nil, err
	}
	return parseIDSet(out), nil
}

func (s *DockerContainers) dockerContainerLogsByNamePrefix(ctx context.Context, worker framework.WorkerInfo) (string, error) {
	ids, err := s.dockerContainerIDsByNamePrefixAll(ctx, worker)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("no Docker containers found with prefix %s on worker %s", s.containerNamePrefix, worker.Name)
	}

	logs := make([]string, 0, len(ids))
	for id := range ids {
		out, err := s.runtime.Worker(worker).RunWithDefaultRetry(ctx,
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

func graphDriverPathsUnder(output, root string) bool {
	foundPath := false
	for _, line := range strings.Split(output, "\n") {
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}
		for _, field := range strings.Split(value, ":") {
			candidate := strings.TrimSpace(field)
			if candidate == "" || !strings.HasPrefix(candidate, "/") {
				continue
			}
			foundPath = true
			if !pathIsUnder(candidate, root) {
				return false
			}
		}
	}
	return foundPath
}

func dockerCgroupParentBelongsToJob(value, jobID string) bool {
	cleaned := path.Clean(strings.TrimSpace(value))
	if cleaned == "." || !strings.HasSuffix(cleaned, "/user") {
		return false
	}

	for _, component := range strings.Split(strings.TrimPrefix(cleaned, "/"), "/") {
		if component == "job_"+jobID {
			return true
		}
	}
	return false
}

func pathIsUnder(value, root string) bool {
	cleanValue := path.Clean(strings.TrimSpace(value))
	cleanRoot := path.Clean(strings.TrimSpace(root))
	return cleanValue == cleanRoot || strings.HasPrefix(cleanValue, cleanRoot+"/")
}

func (s *DockerContainers) stopContainersByNamePrefix(ctx context.Context) {
	if s.containerNamePrefix == "" {
		return
	}

	for _, worker := range s.workers {
		out, err := s.runtime.Worker(worker).RunWithDefaultRetry(ctx,
			fmt.Sprintf("sudo docker ps --filter name=%s --format '{{.ID}}'", framework.ShellQuote(s.containerNamePrefix)))
		if err != nil {
			s.runtime.Logf("Docker cleanup: list containers on worker %s failed: %v", worker.Name, err)
			continue
		}
		for id := range parseIDSet(out) {
			if _, err := s.runtime.Worker(worker).Run(ctx, fmt.Sprintf("sudo docker stop %s >/dev/null 2>&1 || true", framework.ShellQuote(id))); err != nil {
				s.runtime.Logf("Docker cleanup: stop container %s on worker %s failed: %v", id, worker.Name, err)
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
			s.runtime.Logf("Docker cleanup: list all containers on worker %s failed: %v", worker.Name, err)
			continue
		}
		for id := range ids {
			if _, err := s.runtime.Worker(worker).Run(ctx, fmt.Sprintf("sudo docker rm -f %s >/dev/null 2>&1 || true", framework.ShellQuote(id))); err != nil {
				s.runtime.Logf("Docker cleanup: remove container %s on worker %s failed: %v", id, worker.Name, err)
			}
		}
	}
}
