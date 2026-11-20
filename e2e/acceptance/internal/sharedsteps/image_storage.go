package sharedsteps

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	imageStorageSSHKeyName    = "soperator_e2e_image_storage_ssh"
	imageStorageSSHKeyComment = "soperator-e2e-image-storage-ssh"

	imageStorageCommandTimeout = 8 * time.Minute
	imageStorageRestartTimeout = 10 * time.Minute
	imageStorageUsableTimeout  = 5 * time.Minute
	imageStorageSSHTimeout     = 3 * time.Minute
	imageStorageCleanupTimeout = 30 * time.Minute

	imageStorageContainerRoot = "/soperator-e2e"

	// Pro names the init container exactly so; Managed appends the filesystem type.
	imageStoragePrepareContainerPrefix = "prepare-node-local-image-storage"

	// Linux refuses a single path argument longer than PATH_MAX, so the tree is built from two halves that
	// each fit and are joined with a rename, as a recursive copy or move inside a container would do.
	imageStoragePathMax           = 4096
	imageStorageDeepSegmentLength = 200
	imageStorageDeepLevelsPerHalf = 12

	// A setuid file, a private file and a sticky directory: a recursive chmod flattens all three.
	imageStorageMarkerModes = "4755 600 1777"

	imageStorageContentCreatedNow     = "now"
	imageStorageContentCreatedEarlier = "earlier"
)

type imageStorageScopeKey struct{}

// ImageStorage covers a worker restart with user-created content on the node-local image-storage disk.
type ImageStorage struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	worker framework.WorkerInfo
	pod    framework.WorkerPodInfo

	identityUsers    []string
	nonce            string
	containerName    string
	containerCreated bool
	imageIDBefore    string
	enrootContainer  bool

	podUIDBeforeRestart string
}

func NewImageStorage(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient,
	selector *framework.WorkerSelector,
) *ImageStorage {
	return &ImageStorage{
		info:     info,
		runtime:  runtime,
		slurm:    slurm,
		kubectl:  kubectl,
		selector: selector,
	}
}

func (s *ImageStorage) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a healthy worker with node-local image storage is acquired$`, s.acquireWorker)
	sc.Step(`^two container test users exist$`, s.twoContainerTestUsersExist)
	sc.Step(`^the first user has imported an Enroot image and run a Docker container on the worker$`, s.theFirstUserUsesBothRuntimes)
	sc.Step(`^a Docker container of the first user holds a directory tree longer than the path limit$`, s.aDockerContainerHoldsADeepTree)
	sc.Step(`^the worker pod is restarted$`, s.theWorkerPodIsRestarted)
	sc.Step(`^the worker returns to service$`, s.theWorkerReturnsToService)
	sc.Step(`^the Docker image and the deep directory tree are preserved with their permissions$`, s.dockerContentIsPreserved)
	sc.Step(`^the second user runs the same Enroot image and the same Docker image on the worker$`, s.theSecondUserUsesBothRuntimes)
	// The shared suite has not enabled native reservations for every scenario yet.
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if ctx.Value(imageStorageScopeKey{}) != nil {
			s.CleanupAndReset(context.WithoutCancel(ctx))
			return ctx, framework.FinishWorkerReservation(ctx, s.runtime)
		}
		return ctx, nil
	})
}

func (s *ImageStorage) CleanupAndReset(ctx context.Context) {
	defer s.reset()
	if s.worker.Name == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, imageStorageCleanupTimeout)
	defer cancel()

	if s.podUIDBeforeRestart != "" {
		if err := s.restoreWorker(ctx); err != nil {
			framework.RecordWorkerCleanupError(ctx, err)
			s.runtime.Logf("cleanup: restore worker %s after image-storage restart: %v", s.worker.Name, err)
		}
	}
	if s.containerCreated {
		command := fmt.Sprintf("docker rm -f %s >/dev/null 2>&1 || true", framework.ShellQuote(s.containerName))
		if _, err := s.runtime.Worker(s.worker).Run(ctx, command); err != nil {
			s.runtime.Logf("cleanup: remove Docker container %s from %s: %v", s.containerName, s.worker.Name, err)
		}
	}
	if s.enrootContainer {
		command := fmt.Sprintf("enroot remove -f %s", framework.ShellQuote(s.enrootContainerName()))
		if _, err := s.runAs(ctx, dockerSSHUserName, command); err != nil {
			s.runtime.Logf("cleanup: remove Enroot container %s from %s: %v", s.enrootContainerName(), s.worker.Name, err)
		}
	}
	for _, user := range s.identityUsers {
		// Homes are shared with the login node, so the images go away even when the worker is unreachable.
		command := runAsLoginUser(user, "rm -f -- "+s.enrootImagePath())
		if _, err := s.runtime.Jail().Run(ctx, command); err != nil {
			s.runtime.Logf("cleanup: remove Enroot image of %s: %v", user, err)
		}
		if err := removeSSHTestIdentity(ctx, s.runtime, user, imageStorageSSHKeyName, imageStorageSSHKeyComment); err != nil {
			s.runtime.Logf("cleanup: remove image-storage SSH identity of %s: %v", user, err)
		}
	}
}

func (s *ImageStorage) reset() {
	s.worker = framework.WorkerInfo{}
	s.pod = framework.WorkerPodInfo{}
	s.identityUsers = nil
	s.nonce = ""
	s.containerName = ""
	s.containerCreated = false
	s.imageIDBefore = ""
	s.enrootContainer = false
	s.podUIDBeforeRestart = ""
}

func (s *ImageStorage) acquireWorker(ctx context.Context) (context.Context, error) {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return ctx, err
	}
	if !anyNodeSetHasImageStorage(snapshot.NodeSets) {
		s.runtime.Logf("acceptance: no Docker-enabled NodeSet has an %s sub-mount, skipping scenario", containerRuntimeSubmountName)
		return ctx, godog.ErrSkip
	}

	if !framework.HasWorkerReservation(ctx) {
		ctx = context.WithValue(ctx, imageStorageScopeKey{}, true)
		ctx = framework.WithWorkerReservation(ctx, framework.NewReservationManager(), "image storage worker restart")
	}
	workers, err := s.selector.Acquire(ctx, framework.WorkerRequirements{Count: 1, Docker: true, Idle: true})
	if err != nil {
		return ctx, framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	nodeSet, err := nodeSetForWorker(ctx, s.kubectl, s.info.SlurmClusterName, workers[0])
	if err != nil {
		return ctx, err
	}
	if !nodeSetHasImageStorage(nodeSet) {
		return ctx, fmt.Errorf("worker %s of Docker-enabled NodeSet %s has no %s sub-mount: %s",
			workers[0].Name, nodeSet.Name, containerRuntimeSubmountName, describeJailSubMounts(nodeSet.JailSubMounts))
	}
	pod, err := s.kubectl.WorkerPodForSlurmNode(ctx, workers[0].Name)
	if err != nil {
		return ctx, err
	}

	s.worker = workers[0]
	s.pod = pod
	s.nonce = strings.ToLower(rand.Text())
	s.containerName = "soperator-e2e-image-storage-" + s.nonce
	s.runtime.Logf("image storage: acquired worker=%s pod=%s nodeset=%s", s.worker.Name, s.pod.PodName, nodeSet.Name)
	return ctx, nil
}

func anyNodeSetHasImageStorage(nodeSets []framework.NodeSetInfo) bool {
	for _, nodeSet := range nodeSets {
		if nodeSetHasImageStorage(nodeSet) {
			return true
		}
	}
	return false
}

func nodeSetHasImageStorage(nodeSet framework.NodeSetInfo) bool {
	if !nodeSet.DockerEnabled {
		return false
	}
	for _, mount := range nodeSet.JailSubMounts {
		if mount.Name == containerRuntimeSubmountName {
			return true
		}
	}
	return false
}

func (s *ImageStorage) twoContainerTestUsersExist(ctx context.Context) error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	for _, user := range []string{sshTestUserName, dockerSSHUserName} {
		if err := ensureSSHTestUser(ctx, s.runtime, user); err != nil {
			return err
		}
		s.identityUsers = append(s.identityUsers, user)
		if err := ensureSSHTestIdentity(ctx, s.runtime, user, imageStorageSSHKeyName, imageStorageSSHKeyComment); err != nil {
			return err
		}
		if err := waitForSSHTestUserInGroupOnWorker(ctx, s.runtime, user, "docker", s.worker); err != nil {
			return err
		}
	}
	return nil
}

func (s *ImageStorage) theFirstUserUsesBothRuntimes(ctx context.Context) error {
	if err := s.requireUsers(); err != nil {
		return err
	}
	if _, err := s.runAs(ctx, sshTestUserName, s.enrootImportCommand()); err != nil {
		return fmt.Errorf("import Enroot image as %s on worker %s: %w", sshTestUserName, s.worker.Name, err)
	}
	output, err := s.runAs(ctx, sshTestUserName, dockerOSReleaseCommand())
	if err != nil {
		return fmt.Errorf("run Docker container as %s on worker %s: %w", sshTestUserName, s.worker.Name, err)
	}
	if id := osReleaseID(output); id != "ubuntu" {
		return fmt.Errorf("Docker container of %s on worker %s reports OS ID %q, want %q; output: %s",
			sshTestUserName, s.worker.Name, id, "ubuntu", strings.TrimSpace(output))
	}
	s.imageIDBefore, err = s.dockerImageID(ctx)
	return err
}

func (s *ImageStorage) aDockerContainerHoldsADeepTree(ctx context.Context) error {
	if err := s.requireUsers(); err != nil {
		return err
	}
	// The container is kept after it exits, so its writable layer stays in Docker's storage across the restart.
	s.containerCreated = true
	run := fmt.Sprintf("docker run --name %s %s bash -c %s",
		framework.ShellQuote(s.containerName), framework.ShellQuote(ubuntuDockerImage), framework.ShellQuote(imageStorageContainerScript()))
	output, err := s.runAs(ctx, sshTestUserName, run)
	if err != nil {
		return fmt.Errorf("create deep directory tree in Docker container %s on worker %s: %w", s.containerName, s.worker.Name, err)
	}
	return s.checkContainerContent(output, imageStorageContentCreatedNow)
}

func (s *ImageStorage) theWorkerPodIsRestarted(ctx context.Context) error {
	if err := s.requireUsers(); err != nil {
		return err
	}
	pod, found, err := s.kubectl.PodIfPresent(ctx, framework.SoperatorNamespace, s.pod.PodName)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("restart worker pod %s: pod was not found", s.pod.PodName)
	}
	s.podUIDBeforeRestart = string(pod.UID)
	if _, err := s.runtime.Kubectl().Run(ctx,
		"delete", "pod", s.pod.PodName,
		"-n", framework.SoperatorNamespace,
		"--wait=false",
	); err != nil {
		return fmt.Errorf("restart worker pod %s: %w", s.pod.PodName, err)
	}
	return nil
}

func (s *ImageStorage) theWorkerReturnsToService(ctx context.Context) error {
	if s.podUIDBeforeRestart == "" {
		return fmt.Errorf("worker pod restart was not started")
	}
	if err := s.waitForWorkerPod(ctx, imageStorageRestartTimeout, false); err != nil {
		return err
	}
	if err := s.slurm.WaitForNodeUsable(ctx, s.worker.Name, imageStorageUsableTimeout); err != nil {
		return err
	}
	// Pod readiness precedes sshd and dockerd inside the worker.
	return s.runtime.WaitFor(ctx,
		fmt.Sprintf("Docker reachable over SSH on worker %s", s.worker.Name),
		imageStorageSSHTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			if _, err := s.runAs(waitCtx, sshTestUserName, "docker info >/dev/null"); err != nil {
				return false, err
			}
			return true, nil
		})
}

func (s *ImageStorage) dockerContentIsPreserved(ctx context.Context) error {
	if s.imageIDBefore == "" {
		return fmt.Errorf("Docker image ID was not recorded before the restart")
	}
	imageID, err := s.dockerImageID(ctx)
	if err != nil {
		return err
	}
	if imageID != s.imageIDBefore {
		return fmt.Errorf("Docker image %s on worker %s changed across the restart: before=%s after=%s",
			ubuntuDockerImage, s.worker.Name, s.imageIDBefore, imageID)
	}
	// Starting the kept container again reruns its command, which reports what it finds in its writable layer.
	start := fmt.Sprintf("docker start --attach %s", framework.ShellQuote(s.containerName))
	output, err := s.runAs(ctx, sshTestUserName, start)
	if err != nil {
		return fmt.Errorf("start preserved Docker container %s on worker %s: %w", s.containerName, s.worker.Name, err)
	}
	return s.checkContainerContent(output, imageStorageContentCreatedEarlier)
}

func (s *ImageStorage) theSecondUserUsesBothRuntimes(ctx context.Context) error {
	if err := s.requireUsers(); err != nil {
		return err
	}
	user := dockerSSHUserName
	if _, err := s.runAs(ctx, user, s.enrootImportCommand()); err != nil {
		return fmt.Errorf("import the same Enroot image as %s on worker %s: %w", user, s.worker.Name, err)
	}
	s.enrootContainer = true
	create := fmt.Sprintf("enroot create --name %s %s", framework.ShellQuote(s.enrootContainerName()), s.enrootImagePath())
	if _, err := s.runAs(ctx, user, create); err != nil {
		return fmt.Errorf("create Enroot container as %s on worker %s: %w", user, s.worker.Name, err)
	}
	start := fmt.Sprintf("enroot start %s cat /etc/os-release", framework.ShellQuote(s.enrootContainerName()))
	output, err := s.runAs(ctx, user, start)
	if err != nil {
		return fmt.Errorf("start Enroot container as %s on worker %s: %w", user, s.worker.Name, err)
	}
	if id := osReleaseID(output); id != "ubuntu" {
		return fmt.Errorf("Enroot container of %s on worker %s reports OS ID %q, want %q; output: %s",
			user, s.worker.Name, id, "ubuntu", strings.TrimSpace(output))
	}
	remove := fmt.Sprintf("enroot remove -f %s", framework.ShellQuote(s.enrootContainerName()))
	if _, err := s.runAs(ctx, user, remove); err != nil {
		return fmt.Errorf("remove Enroot container as %s on worker %s: %w", user, s.worker.Name, err)
	}
	s.enrootContainer = false

	output, err = s.runAs(ctx, user, dockerOSReleaseCommand())
	if err != nil {
		return fmt.Errorf("run the same Docker image as %s on worker %s: %w", user, s.worker.Name, err)
	}
	if id := osReleaseID(output); id != "ubuntu" {
		return fmt.Errorf("Docker container of %s on worker %s reports OS ID %q, want %q; output: %s",
			user, s.worker.Name, id, "ubuntu", strings.TrimSpace(output))
	}
	return nil
}

func (s *ImageStorage) requireWorker() error {
	if s.worker.Name == "" || s.pod.PodName == "" {
		return fmt.Errorf("image-storage worker is not acquired")
	}
	return nil
}

func (s *ImageStorage) requireUsers() error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	if len(s.identityUsers) != 2 {
		return fmt.Errorf("container test users are not prepared")
	}
	return nil
}

func (s *ImageStorage) runAs(ctx context.Context, user, remoteCommand string) (string, error) {
	return runSSHCommand(ctx, s.runtime, user, imageStorageSSHKeyName, s.worker.Name, imageStorageCommandTimeout, remoteCommand)
}

// enrootImagePath is relative to the home of whoever runs the command, so both users get their own copy.
func (s *ImageStorage) enrootImagePath() string {
	return "~/.cache/soperator-e2e-image-storage-" + s.nonce + ".sqsh"
}

func (s *ImageStorage) enrootContainerName() string {
	return "soperator-e2e-image-storage-" + s.nonce
}

func (s *ImageStorage) enrootImportCommand() string {
	return fmt.Sprintf("install -d -m 0700 ~/.cache && rm -f -- %s && enroot import --output %s %s",
		s.enrootImagePath(), s.enrootImagePath(), framework.ShellQuote(enrootLifecycleImage))
}

func dockerOSReleaseCommand() string {
	return fmt.Sprintf("docker run --rm %s cat /etc/os-release", framework.ShellQuote(ubuntuDockerImage))
}

func (s *ImageStorage) dockerImageID(ctx context.Context) (string, error) {
	command := fmt.Sprintf("docker image inspect --format '{{.Id}}' %s", framework.ShellQuote(ubuntuDockerImage))
	output, err := s.runAs(ctx, sshTestUserName, command)
	if err != nil {
		return "", fmt.Errorf("inspect Docker image %s on worker %s: %w", ubuntuDockerImage, s.worker.Name, err)
	}
	imageID := strings.TrimSpace(output)
	if imageID == "" {
		return "", fmt.Errorf("inspect Docker image %s on worker %s: empty image ID", ubuntuDockerImage, s.worker.Name)
	}
	return imageID, nil
}

func (s *ImageStorage) checkContainerContent(output, wantCreated string) error {
	probe, err := parseImageStorageProbe(output)
	if err != nil {
		return fmt.Errorf("probe Docker container %s on worker %s: %w", s.containerName, s.worker.Name, err)
	}
	if probe.created != wantCreated {
		return fmt.Errorf("content of Docker container %s on worker %s was created %q, want %q",
			s.containerName, s.worker.Name, probe.created, wantCreated)
	}
	if probe.modes != imageStorageMarkerModes {
		return fmt.Errorf("permissions in Docker container %s on worker %s are %q, want %q",
			s.containerName, s.worker.Name, probe.modes, imageStorageMarkerModes)
	}
	if probe.leaves != 1 {
		return fmt.Errorf("deep directory tree in Docker container %s on worker %s has %d leaf files, want 1",
			s.containerName, s.worker.Name, probe.leaves)
	}
	return nil
}

// imageStorageContainerScript is the command of the kept container. Its first run creates the content;
// every run reports whether the content was already there, its permissions and the reachable leaf files.
// find walks with relative steps, so it reaches the leaf that no single path argument can name.
func imageStorageContainerScript() string {
	return fmt.Sprintf(`set -eu
root=%[1]s
if [ -e "$root/created" ]; then
    echo "created=%[5]s"
else
    segment=$(printf 'd%%.0s' $(seq 1 %[2]d))
    half=""
    for _ in $(seq 1 %[3]d); do half="$half/$segment"; done
    mkdir -p "$root/deep/a$half" "$root/deep/b$half"
    touch "$root/deep/b$half/leaf"
    mv "$root/deep/b" "$root/deep/a$half/b"
    install -m 4755 /dev/null "$root/setuid"
    install -m 0600 /dev/null "$root/private"
    install -d -m 1777 "$root/sticky"
    touch "$root/created"
    echo "created=%[4]s"
fi
echo "modes=$(stat -c %%a "$root/setuid" "$root/private" "$root/sticky" | xargs)"
echo "leaves=$(find "$root/deep" -type f -name leaf | wc -l)"
`, imageStorageContainerRoot, imageStorageDeepSegmentLength, imageStorageDeepLevelsPerHalf,
		imageStorageContentCreatedNow, imageStorageContentCreatedEarlier)
}

// imageStorageDeepTreePathLengths returns the longest path passed to a single command while the tree is
// built and the length of the leaf path after the two halves are joined.
func imageStorageDeepTreePathLengths() (building, joined int) {
	half := imageStorageDeepLevelsPerHalf * (imageStorageDeepSegmentLength + 1)
	prefix := len(imageStorageContainerRoot + "/deep/a")
	building = prefix + half + len("/leaf")
	joined = prefix + half + len("/b") + half + len("/leaf")
	return building, joined
}

type imageStorageProbe struct {
	created string
	modes   string
	leaves  int
}

func parseImageStorageProbe(output string) (imageStorageProbe, error) {
	var probe imageStorageProbe
	var haveCreated, haveModes, haveLeaves bool
	for line := range strings.SplitSeq(output, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		switch key {
		case "created":
			probe.created = strings.TrimSpace(value)
			haveCreated = true
		case "modes":
			probe.modes = strings.Join(strings.Fields(value), " ")
			haveModes = true
		case "leaves":
			leaves, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return imageStorageProbe{}, fmt.Errorf("parse leaf count %q: %w", value, err)
			}
			probe.leaves = leaves
			haveLeaves = true
		}
	}
	if !haveCreated || !haveModes || !haveLeaves {
		return imageStorageProbe{}, fmt.Errorf("parse container probe: created, modes or leaves are missing in output: %s", strings.TrimSpace(output))
	}
	return probe, nil
}

// waitForWorkerPod waits for the pod that replaces the restarted one. With stopOnPrepareFailure it gives up
// as soon as the init container that prepares image storage fails: it fails on what is stored on the disk,
// so retries do not help.
func (s *ImageStorage) waitForWorkerPod(ctx context.Context, timeout time.Duration, stopOnPrepareFailure bool) error {
	observed := "pod was not observed"
	err := s.runtime.WaitFor(ctx,
		fmt.Sprintf("worker pod %s recreated and ready", s.pod.PodName),
		timeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			pod, found, err := s.kubectl.PodIfPresent(waitCtx, framework.SoperatorNamespace, s.pod.PodName)
			if err != nil {
				return false, err
			}
			if !found {
				observed = "pod is absent"
				return false, nil
			}
			observed = describeWorkerPodStartup(pod)
			if podRecreatedAndReady(pod, s.podUIDBeforeRestart) {
				return true, nil
			}
			if stopOnPrepareFailure && string(pod.UID) != s.podUIDBeforeRestart && imageStoragePrepareFailed(pod) {
				return false, &framework.PermanentError{Err: fmt.Errorf("image-storage init container of worker pod %s failed", pod.Name)}
			}
			return false, nil
		})
	if err != nil {
		return fmt.Errorf("%w; last observed: %s", err, observed)
	}
	return nil
}

// imageStoragePrepareFailed ignores other init containers: those wait for mounts and may fail transiently.
func imageStoragePrepareFailed(pod corev1.Pod) bool {
	for _, status := range pod.Status.InitContainerStatuses {
		if !strings.HasPrefix(status.Name, imageStoragePrepareContainerPrefix) {
			continue
		}
		if status.RestartCount > 0 {
			return true
		}
		if terminated := status.State.Terminated; terminated != nil && terminated.ExitCode != 0 {
			return true
		}
	}
	return false
}

func describeWorkerPodStartup(pod corev1.Pod) string {
	parts := []string{fmt.Sprintf("uid=%s phase=%s", pod.UID, pod.Status.Phase)}
	for _, status := range pod.Status.InitContainerStatuses {
		state := "pending"
		switch {
		case status.State.Running != nil:
			state = "running"
		case status.State.Waiting != nil:
			state = "waiting:" + status.State.Waiting.Reason
		case status.State.Terminated != nil:
			if status.State.Terminated.ExitCode == 0 {
				continue
			}
			state = fmt.Sprintf("exit=%d", status.State.Terminated.ExitCode)
		}
		parts = append(parts, fmt.Sprintf("init/%s=%s restarts=%d", status.Name, state, status.RestartCount))
	}
	return strings.Join(parts, " ")
}

// restoreWorker brings the restarted worker back. The scenario leaves a directory tree on the image-storage
// disk that a recursive walk cannot finish; when that keeps the pod from starting, no restart helps, and
// the disk holds nothing but re-downloadable images, so the claim is replaced with an empty one.
func (s *ImageStorage) restoreWorker(ctx context.Context) error {
	waitErr := s.waitForWorkerPod(ctx, imageStorageRestartTimeout, true)
	if waitErr == nil {
		return nil
	}
	claim := containerRuntimeSubmountName + "-" + s.pod.PodName
	s.runtime.Logf("cleanup: worker pod %s did not start (%v), replacing PersistentVolumeClaim %s", s.pod.PodName, waitErr, claim)

	previousClaimUID, err := s.claimUID(ctx, claim)
	if err != nil {
		return errors.Join(waitErr, err)
	}
	if _, err := s.runtime.Kubectl().Run(ctx,
		"delete", "pvc", claim, "-n", framework.SoperatorNamespace, "--wait=false", "--ignore-not-found",
	); err != nil {
		return errors.Join(waitErr, fmt.Errorf("delete PersistentVolumeClaim %s: %w", claim, err))
	}
	if err := s.deleteWorkerPod(ctx); err != nil {
		return errors.Join(waitErr, err)
	}
	err = s.runtime.WaitFor(ctx,
		fmt.Sprintf("worker pod %s ready on a new PersistentVolumeClaim %s", s.pod.PodName, claim),
		imageStorageRestartTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			claimUID, err := s.claimUID(waitCtx, claim)
			if err != nil {
				return false, err
			}
			pod, found, err := s.kubectl.PodIfPresent(waitCtx, framework.SoperatorNamespace, s.pod.PodName)
			if err != nil || !found {
				return false, err
			}
			if claimUID == "" || claimUID == previousClaimUID {
				// A pod created before the old claim was released keeps waiting for it.
				if pod.Status.Phase == corev1.PodPending && pod.DeletionTimestamp == nil && claimUID == "" {
					return false, s.deleteWorkerPod(waitCtx)
				}
				return false, nil
			}
			return podRecreatedAndReady(pod, s.podUIDBeforeRestart), nil
		})
	if err != nil {
		return errors.Join(waitErr, err)
	}
	return s.slurm.WaitForNodeUsable(ctx, s.worker.Name, imageStorageUsableTimeout)
}

func (s *ImageStorage) claimUID(ctx context.Context, claim string) (string, error) {
	output, err := s.runtime.Kubectl().Run(ctx,
		"get", "pvc", claim, "-n", framework.SoperatorNamespace, "--ignore-not-found", "-o", "jsonpath={.metadata.uid}",
	)
	if err != nil {
		return "", fmt.Errorf("read PersistentVolumeClaim %s: %w", claim, err)
	}
	return strings.TrimSpace(output), nil
}

func (s *ImageStorage) deleteWorkerPod(ctx context.Context) error {
	if _, err := s.runtime.Kubectl().Run(ctx,
		"delete", "pod", s.pod.PodName, "-n", framework.SoperatorNamespace, "--wait=false", "--ignore-not-found",
	); err != nil {
		return fmt.Errorf("delete worker pod %s: %w", s.pod.PodName, err)
	}
	return nil
}
