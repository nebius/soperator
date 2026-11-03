package sharedsteps

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	// localNVMeJobImmediate bounds how long srun waits for the targeted worker to become free.
	localNVMeJobImmediate = 2 * time.Minute
	localNVMeJobTimeout   = 3 * time.Minute
	// localNVMePyxisTimeout includes a cold Enroot import of the probe image.
	localNVMePyxisTimeout   = 5 * time.Minute
	localNVMeCleanupTimeout = 30 * time.Second

	localNVMeMarkerDirPrefix = "soperator-e2e-local-nvme-"
	localNVMeMarkerFileName  = "marker"

	localNVMeRootDeviceKey    = "SCRATCH_ROOT_DEV"
	localNVMeDeviceKey        = "SCRATCH_DEV"
	localNVMeFilesystemKey    = "SCRATCH_FSTYPE"
	localNVMeCapacityKey      = "SCRATCH_CAPACITY_KB"
	localNVMeReadbackKey      = "SCRATCH_READBACK"
	localNVMePyxisReadbackKey = "SCRATCH_PYXIS_READBACK"
)

// localNVMeWorker pairs a usable Slurm worker with its Ready worker pod.
type localNVMeWorker struct {
	framework.WorkerInfo
	Pod framework.WorkerPodInfo
}

// localNVMeNodeSet is a NodeSet whose workers expose a local NVMe emptyDir as jail scratch.
type localNVMeNodeSet struct {
	Name string
	// MountPath is the cleaned mountPath of the local-nvme jail submount, as jobs see it.
	MountPath string
	// SizeLimit is the emptyDir sizeLimit of the local-nvme jail submount in the NodeSet CR.
	SizeLimit resource.Quantity
	// SlurmdResources is spec.slurmd.resources of the NodeSet CR.
	SlurmdResources corev1.ResourceList
	// Workers are the usable Slurm workers of the NodeSet with Ready pods, sorted by name.
	Workers []localNVMeWorker
	// UnusableWorkers lists Slurm workers excluded from the scenario because they are
	// down, drained or powered off; cluster health is covered by other scenarios.
	UnusableWorkers []string
}

// localNVMeFilesystem is what the filesystem probe observed for scratch inside a job.
type localNVMeFilesystem struct {
	// RootDevice is the st_dev of the job's / (the shared jail), from stat -c %d.
	RootDevice string
	// ScratchDevice is the st_dev of the scratch mount path, from stat -c %d.
	ScratchDevice string
	// FilesystemType is the scratch filesystem name from stat -f %T (ext4 reports ext2/ext3).
	FilesystemType string
	// CapacityBytes is the scratch filesystem size from df -P, in bytes.
	CapacityBytes uint64
}

// localNVMeMarker is a scratch directory the scenario created on a worker and must remove.
type localNVMeMarker struct {
	Worker framework.WorkerInfo
	// Dir is the absolute jail path of the marker directory under the scratch mount.
	Dir string
}

// LocalNVMe implements the local NVMe scratch scenario steps.
type LocalNVMe struct {
	runtime  framework.Runtime
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	// nodeSets are the discovered NVMe NodeSets, narrowed by the selection step.
	nodeSets []localNVMeNodeSet
	// markerDir is the per-scenario directory name created under each scratch mount.
	markerDir string
	// markers are the directories created so far, removed by CleanupAndReset.
	markers []localNVMeMarker
}

func NewLocalNVMe(runtime framework.Runtime, kubectl *framework.KubectlClient, selector *framework.WorkerSelector) *LocalNVMe {
	return &LocalNVMe{runtime: runtime, kubectl: kubectl, selector: selector}
}

func (s *LocalNVMe) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the cluster has NodeSets with local NVMe scratch$`, s.clusterHasLocalNVMeNodeSets)
	sc.Step(`^at most (\d+) local NVMe NodeSets with at most (\d+) workers each are selected$`, s.atMostNodeSetsAndWorkersAreSelected)
	sc.Step(`^a regular user account is available on the selected local NVMe workers$`, s.regularUserIsAvailableOnLocalNVMeWorkers)
	sc.Step(`^the scratch size limit matches the slurmd ephemeral storage request on every selected local NVMe NodeSet$`, s.scratchSizeLimitMatchesEphemeralStorageRequest)
	sc.Step(`^every selected local NVMe worker mounts scratch on a dedicated filesystem$`, s.everyWorkerMountsScratchOnDedicatedFilesystem)
	sc.Step(`^a regular user's job writes and reads back a file in scratch on every selected local NVMe worker$`, s.userJobWritesAndReadsBackScratchFile)
	sc.Step(`^a regular user's Pyxis job reads the scratch file on every selected local NVMe worker$`, s.userPyxisJobReadsScratchFile)
}

func (s *LocalNVMe) CleanupAndReset(ctx context.Context) {
	for _, marker := range s.markers {
		cleanupCtx, cancel := context.WithTimeout(ctx, localNVMeCleanupTimeout)
		_, err := s.runtime.Worker(marker.Worker).Run(cleanupCtx, "rm -rf -- "+framework.ShellQuote(marker.Dir))
		cancel()
		if err != nil {
			s.runtime.Logf("cleanup: remove local NVMe marker directory %s from worker %s: %v", marker.Dir, marker.Worker.Name, err)
		}
	}
	s.nodeSets = nil
	s.markerDir = ""
	s.markers = nil
}

func (s *LocalNVMe) clusterHasLocalNVMeNodeSets(ctx context.Context) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	pods, err := s.kubectl.WorkerPods(ctx)
	if err != nil {
		return err
	}
	nodeSets, err := discoverLocalNVMeNodeSets(snapshot, pods)
	if err != nil {
		return err
	}
	if len(nodeSets) == 0 {
		s.runtime.Logf("Local NVMe: no NodeSet has a %s jail submount, skipping scenario", framework.LocalNVMeVolumeName)
		return godog.ErrSkip
	}

	s.nodeSets = nodeSets
	s.markerDir = localNVMeMarkerDirPrefix + strconv.FormatInt(time.Now().UnixNano(), 10)
	for _, nodeSet := range nodeSets {
		s.runtime.Logf("Local NVMe: NodeSet %s mounts scratch at %s with size limit %s on %d workers",
			nodeSet.Name, nodeSet.MountPath, nodeSet.SizeLimit.String(), len(nodeSet.Workers))
		if len(nodeSet.UnusableWorkers) > 0 {
			s.runtime.Logf("Local NVMe: NodeSet %s: skipping unusable workers %s", nodeSet.Name, strings.Join(nodeSet.UnusableWorkers, ", "))
		}
	}
	return nil
}

// atMostNodeSetsAndWorkersAreSelected bounds the scenario duration on large
// clusters: later steps run jobs on every selected worker.
func (s *LocalNVMe) atMostNodeSetsAndWorkersAreSelected(ctx context.Context, maxNodeSets, maxWorkers int) error {
	if err := s.requireNodeSets(); err != nil {
		return err
	}
	selected, err := selectLocalNVMeNodeSets(s.nodeSets, maxNodeSets, maxWorkers)
	if err != nil {
		return err
	}
	s.nodeSets = selected
	for _, nodeSet := range selected {
		s.runtime.Logf("Local NVMe: selected NodeSet %s with workers %s", nodeSet.Name, strings.Join(localNVMeWorkerNames(nodeSet.Workers), ", "))
	}
	return nil
}

func (s *LocalNVMe) regularUserIsAvailableOnLocalNVMeWorkers(ctx context.Context) error {
	if err := s.requireNodeSets(); err != nil {
		return err
	}
	var workers []framework.WorkerInfo
	for _, nodeSet := range s.nodeSets {
		for _, worker := range nodeSet.Workers {
			workers = append(workers, worker.WorkerInfo)
		}
	}
	return prepareSSHTestUser(ctx, s.runtime, sshTestUserName, workers...)
}

func (s *LocalNVMe) scratchSizeLimitMatchesEphemeralStorageRequest(ctx context.Context) error {
	if err := s.requireNodeSets(); err != nil {
		return err
	}
	var problems []error
	for _, nodeSet := range s.nodeSets {
		if err := validateLocalNVMeSizeLimit(nodeSet); err != nil {
			problems = append(problems, err)
		}
	}
	problems = append(problems, s.workerProblems(func(nodeSet localNVMeNodeSet, worker localNVMeWorker) error {
		node, present, err := s.kubectl.NodeIfPresent(ctx, worker.Pod.KubernetesNodeName)
		if err != nil {
			return err
		}
		if !present {
			return fmt.Errorf("Kubernetes node %s was not found", worker.Pod.KubernetesNodeName)
		}
		return validateLocalNVMeNodeAllocatable(nodeSet, node)
	})...)
	return joinLocalNVMeProblems("check local NVMe scratch size limits", problems)
}

func (s *LocalNVMe) everyWorkerMountsScratchOnDedicatedFilesystem(ctx context.Context) error {
	if err := s.requireNodeSets(); err != nil {
		return err
	}
	return joinLocalNVMeProblems("check local NVMe scratch filesystems", s.workerProblems(func(nodeSet localNVMeNodeSet, worker localNVMeWorker) error {
		output, err := s.runUserJob(ctx, worker.WorkerInfo, localNVMeJobTimeout,
			framework.ShellJoin("bash", "-c", localNVMeFilesystemProbe(), "bash", nodeSet.MountPath))
		if err != nil {
			return err
		}
		filesystem, err := parseLocalNVMeFilesystemProbe(output)
		if err != nil {
			return err
		}
		if err := validateLocalNVMeFilesystem(nodeSet, filesystem); err != nil {
			return err
		}
		s.runtime.Logf("Local NVMe: worker %s scratch %s is on device %s (%s) with capacity %d bytes, size limit %s",
			worker.Name, nodeSet.MountPath, filesystem.ScratchDevice, filesystem.FilesystemType, filesystem.CapacityBytes, nodeSet.SizeLimit.String())
		return nil
	}))
}

func (s *LocalNVMe) userJobWritesAndReadsBackScratchFile(ctx context.Context) error {
	if err := s.requireNodeSets(); err != nil {
		return err
	}
	return joinLocalNVMeProblems("write and read back files in local NVMe scratch", s.workerProblems(func(nodeSet localNVMeNodeSet, worker localNVMeWorker) error {
		s.markers = append(s.markers, localNVMeMarker{Worker: worker.WorkerInfo, Dir: path.Join(nodeSet.MountPath, s.markerDir)})
		content := localNVMeMarkerContent(s.markerDir, worker.Name)
		output, err := s.runUserJob(ctx, worker.WorkerInfo, localNVMeJobTimeout,
			framework.ShellJoin("bash", "-c", localNVMeWriteProbe(), "bash", nodeSet.MountPath, s.markerDir, localNVMeMarkerFileName, content))
		if err != nil {
			return err
		}
		return expectReadback(output, localNVMeReadbackKey, content)
	}))
}

func (s *LocalNVMe) userPyxisJobReadsScratchFile(ctx context.Context) error {
	if err := s.requireNodeSets(); err != nil {
		return err
	}
	return joinLocalNVMeProblems("read local NVMe scratch files from Pyxis jobs", s.workerProblems(func(nodeSet localNVMeNodeSet, worker localNVMeWorker) error {
		file := path.Join(nodeSet.MountPath, s.markerDir, localNVMeMarkerFileName)
		content := localNVMeMarkerContent(s.markerDir, worker.Name)
		output, err := s.runUserJob(ctx, worker.WorkerInfo, localNVMePyxisTimeout, framework.ShellJoin(
			"--container-image="+enrootLifecycleImage,
			"--container-mounts="+nodeSet.MountPath+":"+nodeSet.MountPath,
			"/bin/sh", "-ec", localNVMePyxisProbe(), "sh", file, content,
		))
		if err != nil {
			return err
		}
		return expectReadback(output, localNVMePyxisReadbackKey, content)
	}))
}

func (s *LocalNVMe) requireNodeSets() error {
	if len(s.nodeSets) == 0 {
		return fmt.Errorf("local NVMe NodeSets are not discovered")
	}
	return nil
}

// workerProblems runs check on every selected worker and prefixes each failure
// with the worker identity.
func (s *LocalNVMe) workerProblems(check func(nodeSet localNVMeNodeSet, worker localNVMeWorker) error) (problems []error) {
	for _, nodeSet := range s.nodeSets {
		for _, worker := range nodeSet.Workers {
			if err := check(nodeSet, worker); err != nil {
				problems = append(problems, fmt.Errorf("worker %s (NodeSet %s, scratch %s): %w", worker.Name, nodeSet.Name, nodeSet.MountPath, err))
			}
		}
	}
	return problems
}

func (s *LocalNVMe) runUserJob(ctx context.Context, worker framework.WorkerInfo, timeout time.Duration, srunArgs string) (output string, err error) {
	jobCtx, cancel := context.WithTimeout(ctx, timeout+time.Minute)
	defer cancel()
	output, err = s.runtime.Jail().Run(jobCtx, localNVMeUserJobCommand(worker.Name, timeout, srunArgs))
	if err != nil {
		return output, fmt.Errorf("run job as %s: %w; output: %s", sshTestUserName, err, strings.TrimSpace(output))
	}
	return output, nil
}

func joinLocalNVMeProblems(operation string, problems []error) error {
	if joined := errors.Join(problems...); joined != nil {
		return fmt.Errorf("%s: %w", operation, joined)
	}
	return nil
}

func expectReadback(output, key, want string) error {
	if got := parseKeyValueLine(output, key); got != want {
		return fmt.Errorf("expected %s %q, observed %q; output: %s", key, want, got, strings.TrimSpace(output))
	}
	return nil
}

func discoverLocalNVMeNodeSets(snapshot framework.WorkerSnapshot, pods []framework.WorkerPodInfo) (nodeSets []localNVMeNodeSet, err error) {
	podsByWorker := make(map[string]framework.WorkerPodInfo, len(pods))
	podsByNodeSet := make(map[string][]framework.WorkerPodInfo)
	for _, pod := range pods {
		podsByWorker[pod.SlurmNodeName] = pod
		podsByNodeSet[pod.NodeSetName] = append(podsByNodeSet[pod.NodeSetName], pod)
	}

	var problems []error
	for _, nodeSet := range snapshot.NodeSets {
		mount, hasMount := localNVMeSubMount(nodeSet.JailSubMounts)
		if !hasMount {
			podsWithVolume := 0
			for _, pod := range podsByNodeSet[nodeSet.Name] {
				if pod.HasLocalNVMe {
					podsWithVolume++
				}
			}
			if podsWithVolume > 0 {
				problems = append(problems, fmt.Errorf("NodeSet %s has no %s jail submount but %d of %d worker pods have the %[2]s volume",
					nodeSet.Name, framework.LocalNVMeVolumeName, podsWithVolume, len(podsByNodeSet[nodeSet.Name])))
			}
			continue
		}

		entry, err := newLocalNVMeNodeSet(nodeSet, mount)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		workers := snapshot.WorkersByNodeSet[nodeSet.Name]
		if len(workers) == 0 && nodeSet.Replicas == 0 {
			continue
		}
		var workerProblems []error
		entry.Workers, entry.UnusableWorkers, workerProblems = localNVMeNodeSetWorkers(nodeSet.Name, workers, podsByWorker)
		problems = append(problems, workerProblems...)
		if len(entry.Workers) == 0 {
			problems = append(problems, fmt.Errorf("NodeSet %s has no usable Slurm workers: %s", nodeSet.Name, strings.Join(entry.UnusableWorkers, ", ")))
			continue
		}
		nodeSets = append(nodeSets, entry)
	}
	if err := joinLocalNVMeProblems("discover local NVMe NodeSets", problems); err != nil {
		return nil, err
	}
	return nodeSets, nil
}

// localNVMeNodeSetWorkers splits the NodeSet's Slurm workers into usable ones
// paired with their pods, unusable ones described for the log, and problems.
func localNVMeNodeSetWorkers(nodeSetName string, workers []framework.WorkerInfo, podsByWorker map[string]framework.WorkerPodInfo) (usable []localNVMeWorker, unusable []string, problems []error) {
	for _, worker := range workers {
		if !worker.IsUsable() {
			unusable = append(unusable, fmt.Sprintf("%s state=%s reason=%s", worker.Name, worker.SlurmNode.State, worker.SlurmNode.Reason))
			continue
		}
		pod, ok := podsByWorker[worker.Name]
		switch {
		case !ok:
			problems = append(problems, fmt.Errorf("NodeSet %s: usable worker %s has no worker pod", nodeSetName, worker.Name))
		case !pod.Ready:
			problems = append(problems, fmt.Errorf("NodeSet %s: worker pod %s of usable worker %s is not Ready", nodeSetName, pod.PodName, worker.Name))
		case !pod.HasLocalNVMe:
			problems = append(problems, fmt.Errorf("NodeSet %s: worker pod %s of %s has no %s volume", nodeSetName, pod.PodName, worker.Name, framework.LocalNVMeVolumeName))
		default:
			usable = append(usable, localNVMeWorker{WorkerInfo: worker, Pod: pod})
		}
	}
	sort.Slice(usable, func(i, j int) bool {
		return usable[i].Name < usable[j].Name
	})
	sort.Strings(unusable)
	return usable, unusable, problems
}

// selectLocalNVMeNodeSets keeps a random subset of at most maxNodeSets NodeSets and
// at most maxWorkers workers in each, sorted by name so logs and errors stay stable.
func selectLocalNVMeNodeSets(nodeSets []localNVMeNodeSet, maxNodeSets, maxWorkers int) (selected []localNVMeNodeSet, err error) {
	if maxNodeSets < 1 || maxWorkers < 1 {
		return nil, fmt.Errorf("select local NVMe NodeSets: limits must be positive, got %d NodeSets and %d workers", maxNodeSets, maxWorkers)
	}
	for _, i := range rand.Perm(len(nodeSets))[:min(maxNodeSets, len(nodeSets))] {
		nodeSet := nodeSets[i]
		var workers []localNVMeWorker
		for _, j := range rand.Perm(len(nodeSet.Workers))[:min(maxWorkers, len(nodeSet.Workers))] {
			workers = append(workers, nodeSet.Workers[j])
		}
		sort.Slice(workers, func(a, b int) bool {
			return workers[a].Name < workers[b].Name
		})
		nodeSet.Workers = workers
		selected = append(selected, nodeSet)
	}
	sort.Slice(selected, func(a, b int) bool {
		return selected[a].Name < selected[b].Name
	})
	return selected, nil
}

func localNVMeWorkerNames(workers []localNVMeWorker) []string {
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		names = append(names, worker.Name)
	}
	return names
}

func localNVMeSubMount(mounts []framework.JailSubMountInfo) (mount framework.JailSubMountInfo, found bool) {
	for _, candidate := range mounts {
		if candidate.Name == framework.LocalNVMeVolumeName {
			return candidate, true
		}
	}
	return framework.JailSubMountInfo{}, false
}

func newLocalNVMeNodeSet(nodeSet framework.NodeSetInfo, mount framework.JailSubMountInfo) (entry localNVMeNodeSet, err error) {
	if mount.ReadOnly {
		return localNVMeNodeSet{}, fmt.Errorf("NodeSet %s: %s jail submount at %s is read-only", nodeSet.Name, mount.Name, mount.MountPath)
	}
	mountPath, err := cleanAbsolutePath(mount.MountPath)
	if err != nil {
		return localNVMeNodeSet{}, fmt.Errorf("NodeSet %s: validate %s jail submount: %w", nodeSet.Name, mount.Name, err)
	}
	sizeLimit, err := localNVMeEmptyDirSizeLimit(mount)
	if err != nil {
		return localNVMeNodeSet{}, fmt.Errorf("NodeSet %s: %w", nodeSet.Name, err)
	}
	return localNVMeNodeSet{Name: nodeSet.Name, MountPath: mountPath, SizeLimit: sizeLimit, SlurmdResources: nodeSet.SlurmdResources}, nil
}

func localNVMeEmptyDirSizeLimit(mount framework.JailSubMountInfo) (resource.Quantity, error) {
	if mount.VolumeSource == nil || mount.VolumeSource.EmptyDir == nil {
		return resource.Quantity{}, fmt.Errorf("%s jail submount at %s is not an emptyDir volume", mount.Name, mount.MountPath)
	}
	sizeLimit := mount.VolumeSource.EmptyDir.SizeLimit
	if sizeLimit == nil || sizeLimit.IsZero() {
		return resource.Quantity{}, fmt.Errorf("%s jail submount at %s has no emptyDir size limit", mount.Name, mount.MountPath)
	}
	return sizeLimit.DeepCopy(), nil
}

func validateLocalNVMeSizeLimit(nodeSet localNVMeNodeSet) error {
	request, ok := nodeSet.SlurmdResources[corev1.ResourceEphemeralStorage]
	if !ok {
		return fmt.Errorf("NodeSet %s requests no ephemeral-storage for slurmd, expected %s to match the scratch size limit",
			nodeSet.Name, nodeSet.SizeLimit.String())
	}
	if nodeSet.SizeLimit.Cmp(request) != 0 {
		return fmt.Errorf("NodeSet %s: scratch size limit %s does not match slurmd ephemeral-storage request %s",
			nodeSet.Name, nodeSet.SizeLimit.String(), request.String())
	}
	return nil
}

func validateLocalNVMeNodeAllocatable(nodeSet localNVMeNodeSet, node corev1.Node) error {
	allocatable, ok := node.Status.Allocatable[corev1.ResourceEphemeralStorage]
	if !ok {
		return fmt.Errorf("Kubernetes node %s reports no allocatable ephemeral-storage", node.Name)
	}
	if nodeSet.SizeLimit.Cmp(allocatable) > 0 {
		return fmt.Errorf("scratch size limit %s exceeds allocatable ephemeral-storage %s of Kubernetes node %s",
			nodeSet.SizeLimit.String(), allocatable.String(), node.Name)
	}
	return nil
}

func validateLocalNVMeFilesystem(nodeSet localNVMeNodeSet, filesystem localNVMeFilesystem) error {
	if filesystem.ScratchDevice == filesystem.RootDevice {
		return fmt.Errorf("scratch is on the same filesystem as the jail root (device %s, type %s)",
			filesystem.ScratchDevice, filesystem.FilesystemType)
	}
	if sizeLimit := nodeSet.SizeLimit.Value(); sizeLimit > 0 && filesystem.CapacityBytes < uint64(sizeLimit) {
		return fmt.Errorf("scratch capacity %d bytes is below the size limit %s", filesystem.CapacityBytes, nodeSet.SizeLimit.String())
	}
	return nil
}

// localNVMeUserJobCommand runs srunArgs as the test user in a single-task job
// pinned to workerName. The outer timeout bounds the whole job and --immediate
// turns a busy worker into a fast, explicit allocation failure.
func localNVMeUserJobCommand(workerName string, timeout time.Duration, srunArgs string) string {
	srun := fmt.Sprintf("timeout %d srun --immediate=%d --nodes=1 --ntasks=1 --nodelist=%s %s",
		int(timeout.Seconds()),
		int(localNVMeJobImmediate.Seconds()),
		framework.ShellQuote(workerName),
		srunArgs,
	)
	return runAsLoginUser(sshTestUserName, srun)
}

// localNVMeFilesystemProbe reports the device and capacity of the scratch path $1.
func localNVMeFilesystemProbe() string {
	return `set -eu
test -d "$1"
printf '` + localNVMeRootDeviceKey + `=%s\n' "$(stat -c %d /)"
printf '` + localNVMeDeviceKey + `=%s\n' "$(stat -c %d -- "$1")"
printf '` + localNVMeFilesystemKey + `=%s\n' "$(stat -f -c %T -- "$1")"
printf '` + localNVMeCapacityKey + `=%s\n' "$(df -P -k -- "$1" | awk 'NR == 2 { print $2 }')"
`
}

// localNVMeWriteProbe writes content $4 into $1/$2/$3 and prints it back.
func localNVMeWriteProbe() string {
	return `set -eu
dir="$1/$2"
mkdir -p -- "$dir"
printf '%s' "$4" > "$dir/$3"
sync -- "$dir/$3"
printf '` + localNVMeReadbackKey + `=%s\n' "$(cat -- "$dir/$3")"
`
}

// localNVMePyxisProbe verifies that file $1 holds content $2 inside a container.
func localNVMePyxisProbe() string {
	return `set -eu
test "$(cat -- "$1")" = "$2"
printf '` + localNVMePyxisReadbackKey + `=%s\n' "$(cat -- "$1")"
`
}

func localNVMeMarkerContent(markerDir, workerName string) string {
	return workerName + "@" + markerDir
}

func parseLocalNVMeFilesystemProbe(output string) (filesystem localNVMeFilesystem, err error) {
	values := make(map[string]string, 4)
	for _, key := range []string{localNVMeRootDeviceKey, localNVMeDeviceKey, localNVMeFilesystemKey, localNVMeCapacityKey} {
		if values[key] = parseKeyValueLine(output, key); values[key] == "" {
			return localNVMeFilesystem{}, fmt.Errorf("parse scratch filesystem probe: %s is missing; output: %s", key, strings.TrimSpace(output))
		}
	}
	capacityKiB, err := strconv.ParseUint(values[localNVMeCapacityKey], 10, 64)
	if err != nil {
		return localNVMeFilesystem{}, fmt.Errorf("parse scratch filesystem probe: %s=%q is not a number", localNVMeCapacityKey, values[localNVMeCapacityKey])
	}
	return localNVMeFilesystem{
		RootDevice:     values[localNVMeRootDeviceKey],
		ScratchDevice:  values[localNVMeDeviceKey],
		FilesystemType: values[localNVMeFilesystemKey],
		CapacityBytes:  capacityKiB * 1024,
	}, nil
}
