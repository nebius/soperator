package sharedsteps

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func localNVMeTestMount(mountPath, sizeLimit string) framework.JailSubMountInfo {
	mount := framework.JailSubMountInfo{
		Name:         framework.LocalNVMeVolumeName,
		MountPath:    mountPath,
		VolumeSource: &corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
	if sizeLimit != "" {
		quantity := resource.MustParse(sizeLimit)
		mount.VolumeSource.EmptyDir.SizeLimit = &quantity
	}
	return mount
}

func localNVMeTestNodeSet(name, request string, replicas int, mounts ...framework.JailSubMountInfo) framework.NodeSetInfo {
	nodeSet := framework.NodeSetInfo{Name: name, Replicas: replicas, JailSubMounts: mounts}
	if request != "" {
		nodeSet.SlurmdResources = corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse(request)}
	}
	return nodeSet
}

func localNVMeTestWorker(name, nodeSet, state string) framework.WorkerInfo {
	return framework.WorkerInfo{Name: name, NodeSetName: nodeSet, SlurmNode: framework.SlurmNodeInfo{Name: name, State: state, Reason: "none"}}
}

func localNVMeTestPod(worker, nodeSet string, ready, hasNVMe bool) framework.WorkerPodInfo {
	return framework.WorkerPodInfo{
		SlurmNodeName:      worker,
		PodName:            "pod-" + worker,
		NodeSetName:        nodeSet,
		KubernetesNodeName: "node-" + worker,
		Ready:              ready,
		HasLocalNVMe:       hasNVMe,
	}
}

func localNVMeTestSnapshot(nodeSets []framework.NodeSetInfo, workers ...framework.WorkerInfo) framework.WorkerSnapshot {
	snapshot := framework.WorkerSnapshot{NodeSets: nodeSets, WorkersByNodeSet: map[string][]framework.WorkerInfo{}}
	for _, worker := range workers {
		snapshot.WorkersByNodeSet[worker.NodeSetName] = append(snapshot.WorkersByNodeSet[worker.NodeSetName], worker)
	}
	return snapshot
}

func localNVMeTestNames(nodeSets []localNVMeNodeSet) map[string][]string {
	out := map[string][]string{}
	for _, nodeSet := range nodeSets {
		out[nodeSet.Name] = localNVMeWorkerNames(nodeSet.Workers)
	}
	return out
}

func TestDiscoverLocalNVMeNodeSets(t *testing.T) {
	gb300 := localNVMeTestNodeSet("worker-rack0", "11263Gi", 2, localNVMeTestMount("/scratch", "11263Gi"))
	cpu := localNVMeTestNodeSet("worker-cpu", "100Gi", 1)
	gb300Workers := []framework.WorkerInfo{
		localNVMeTestWorker("worker-rack0-1", "worker-rack0", "IDLE"),
		localNVMeTestWorker("worker-rack0-0", "worker-rack0", "IDLE"),
	}
	gb300Pods := []framework.WorkerPodInfo{
		localNVMeTestPod("worker-rack0-0", "worker-rack0", true, true),
		localNVMeTestPod("worker-rack0-1", "worker-rack0", true, true),
	}
	down := "DOWN+CLOUD+NOT_RESPONDING"

	for _, tc := range []struct {
		name         string
		snapshot     framework.WorkerSnapshot
		pods         []framework.WorkerPodInfo
		wantWorkers  map[string][]string
		wantUnusable []string
		wantErr      string
	}{
		{
			name:        "no NVMe NodeSets",
			snapshot:    localNVMeTestSnapshot([]framework.NodeSetInfo{cpu}, localNVMeTestWorker("worker-cpu-0", "worker-cpu", "IDLE")),
			pods:        []framework.WorkerPodInfo{localNVMeTestPod("worker-cpu-0", "worker-cpu", true, false)},
			wantWorkers: map[string][]string{},
		},
		{
			name: "mixed NVMe and CPU NodeSets with sorted workers",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{cpu, gb300},
				append(gb300Workers, localNVMeTestWorker("worker-cpu-0", "worker-cpu", "IDLE"))...),
			pods:        append(gb300Pods, localNVMeTestPod("worker-cpu-0", "worker-cpu", true, false)),
			wantWorkers: map[string][]string{"worker-rack0": {"worker-rack0-0", "worker-rack0-1"}},
		},
		{
			name:        "NVMe NodeSet scaled to zero is ignored",
			snapshot:    localNVMeTestSnapshot([]framework.NodeSetInfo{localNVMeTestNodeSet("worker-rack0", "11263Gi", 0, localNVMeTestMount("/scratch", "11263Gi"))}),
			wantWorkers: map[string][]string{},
		},
		{
			name: "down worker with pending pod is excluded",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{gb300},
				localNVMeTestWorker("worker-rack0-0", "worker-rack0", down),
				localNVMeTestWorker("worker-rack0-1", "worker-rack0", "IDLE")),
			pods: []framework.WorkerPodInfo{
				localNVMeTestPod("worker-rack0-0", "worker-rack0", false, true),
				localNVMeTestPod("worker-rack0-1", "worker-rack0", true, true),
			},
			wantWorkers:  map[string][]string{"worker-rack0": {"worker-rack0-1"}},
			wantUnusable: []string{"worker-rack0-0 state=" + down + " reason=none"},
		},
		{
			name: "all workers unusable",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{gb300},
				localNVMeTestWorker("worker-rack0-0", "worker-rack0", "IDLE+DRAIN"),
				localNVMeTestWorker("worker-rack0-1", "worker-rack0", down)),
			pods:    gb300Pods,
			wantErr: "NodeSet worker-rack0 has no usable Slurm workers: worker-rack0-0 state=IDLE+DRAIN reason=none, worker-rack0-1 state=" + down + " reason=none",
		},
		{
			name:     "configured NodeSet without workers",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{gb300}),
			wantErr:  "NodeSet worker-rack0 has no usable Slurm workers",
		},
		{
			name:     "pod volume without NodeSet submount",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{cpu}, localNVMeTestWorker("worker-cpu-0", "worker-cpu", "IDLE")),
			pods:     []framework.WorkerPodInfo{localNVMeTestPod("worker-cpu-0", "worker-cpu", true, true)},
			wantErr:  "NodeSet worker-cpu has no local-nvme jail submount but 1 of 1 worker pods have the local-nvme volume",
		},
		{
			name:     "pod without volume",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{gb300}, gb300Workers...),
			pods: []framework.WorkerPodInfo{
				localNVMeTestPod("worker-rack0-0", "worker-rack0", true, true),
				localNVMeTestPod("worker-rack0-1", "worker-rack0", true, false),
			},
			wantErr: "worker pod pod-worker-rack0-1 of worker-rack0-1 has no local-nvme volume",
		},
		{
			name:     "pod of usable worker not ready",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{gb300}, gb300Workers...),
			pods: []framework.WorkerPodInfo{
				localNVMeTestPod("worker-rack0-0", "worker-rack0", false, true),
				localNVMeTestPod("worker-rack0-1", "worker-rack0", true, true),
			},
			wantErr: "worker pod pod-worker-rack0-0 of usable worker worker-rack0-0 is not Ready",
		},
		{
			name:     "usable worker without pod",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{gb300}, gb300Workers...),
			pods:     gb300Pods[:1],
			wantErr:  "usable worker worker-rack0-1 has no worker pod",
		},
		{
			name: "read-only submount",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{localNVMeTestNodeSet("worker-rack0", "11263Gi", 2,
				framework.JailSubMountInfo{Name: framework.LocalNVMeVolumeName, MountPath: "/scratch", ReadOnly: true})}, gb300Workers...),
			pods:    gb300Pods,
			wantErr: "local-nvme jail submount at /scratch is read-only",
		},
		{
			name: "hostPath submount",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{localNVMeTestNodeSet("worker-rack0", "11263Gi", 2,
				framework.JailSubMountInfo{Name: framework.LocalNVMeVolumeName, MountPath: "/scratch",
					VolumeSource: &corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/mnt/local-nvme"}}})}, gb300Workers...),
			pods:    gb300Pods,
			wantErr: "local-nvme jail submount at /scratch is not an emptyDir volume",
		},
		{
			name:     "missing size limit",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{localNVMeTestNodeSet("worker-rack0", "11263Gi", 2, localNVMeTestMount("/scratch", ""))}, gb300Workers...),
			pods:     gb300Pods,
			wantErr:  "local-nvme jail submount at /scratch has no emptyDir size limit",
		},
		{
			name:     "mount path with parent traversal",
			snapshot: localNVMeTestSnapshot([]framework.NodeSetInfo{localNVMeTestNodeSet("worker-rack0", "11263Gi", 2, localNVMeTestMount("/scratch/../data", "11263Gi"))}, gb300Workers...),
			pods:     gb300Pods,
			wantErr:  "parent traversal is not allowed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodeSets, err := discoverLocalNVMeNodeSets(tc.snapshot, tc.pods)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantWorkers, localNVMeTestNames(nodeSets))
			for _, nodeSet := range nodeSets {
				assert.Equal(t, "/scratch", nodeSet.MountPath)
				assert.Equal(t, "11263Gi", nodeSet.SizeLimit.String())
				assert.Equal(t, tc.wantUnusable, nodeSet.UnusableWorkers)
				for _, worker := range nodeSet.Workers {
					assert.Equal(t, "pod-"+worker.Name, worker.Pod.PodName)
				}
			}
		})
	}
}

func TestValidateLocalNVMeSizeLimit(t *testing.T) {
	nodeSet := func(request string) localNVMeNodeSet {
		out := localNVMeNodeSet{Name: "worker", SizeLimit: resource.MustParse("11263Gi")}
		if request != "" {
			out.SlurmdResources = corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse(request)}
		}
		return out
	}

	assert.NoError(t, validateLocalNVMeSizeLimit(nodeSet("11263Gi")))
	assert.ErrorContains(t, validateLocalNVMeSizeLimit(nodeSet("11000Gi")),
		"NodeSet worker: scratch size limit 11263Gi does not match slurmd ephemeral-storage request 11000Gi")
	assert.ErrorContains(t, validateLocalNVMeSizeLimit(nodeSet("")),
		"NodeSet worker requests no ephemeral-storage for slurmd")
}

func TestValidateLocalNVMeNodeAllocatable(t *testing.T) {
	nodeSet := localNVMeNodeSet{Name: "worker", SizeLimit: resource.MustParse("11263Gi")}
	node := func(allocatable string) corev1.Node {
		out := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}}
		if allocatable != "" {
			out.Status.Allocatable = corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse(allocatable)}
		}
		return out
	}

	assert.NoError(t, validateLocalNVMeNodeAllocatable(nodeSet, node("13715149996260")))
	assert.NoError(t, validateLocalNVMeNodeAllocatable(nodeSet, node("11263Gi")))
	assert.ErrorContains(t, validateLocalNVMeNodeAllocatable(nodeSet, node("11000Gi")),
		"scratch size limit 11263Gi exceeds allocatable ephemeral-storage 11000Gi of Kubernetes node node-a")
	assert.ErrorContains(t, validateLocalNVMeNodeAllocatable(nodeSet, node("")),
		"Kubernetes node node-a reports no allocatable ephemeral-storage")
}

func TestValidateLocalNVMeFilesystem(t *testing.T) {
	nodeSet := localNVMeNodeSet{Name: "worker", MountPath: "/scratch", SizeLimit: resource.MustParse("10Gi")}

	assert.NoError(t, validateLocalNVMeFilesystem(nodeSet, localNVMeFilesystem{RootDevice: "1", ScratchDevice: "2", CapacityBytes: 11 << 30}))
	assert.ErrorContains(t, validateLocalNVMeFilesystem(nodeSet, localNVMeFilesystem{RootDevice: "1", ScratchDevice: "1", FilesystemType: "ext2/ext3", CapacityBytes: 11 << 30}),
		"scratch is on the same filesystem as the jail root (device 1, type ext2/ext3)")
	assert.ErrorContains(t, validateLocalNVMeFilesystem(nodeSet, localNVMeFilesystem{RootDevice: "1", ScratchDevice: "2", CapacityBytes: 9 << 30}),
		"scratch capacity 9663676416 bytes is below the size limit 10Gi")
}

func TestParseLocalNVMeFilesystemProbe(t *testing.T) {
	filesystem, err := parseLocalNVMeFilesystemProbe("SCRATCH_ROOT_DEV=64769\nSCRATCH_DEV=2319\nSCRATCH_FSTYPE=ext2/ext3\nSCRATCH_CAPACITY_KB=14883055296\n")
	require.NoError(t, err)
	assert.Equal(t, localNVMeFilesystem{RootDevice: "64769", ScratchDevice: "2319", FilesystemType: "ext2/ext3", CapacityBytes: 14883055296 * 1024}, filesystem)

	_, err = parseLocalNVMeFilesystemProbe("SCRATCH_ROOT_DEV=1\nSCRATCH_DEV=2\nSCRATCH_CAPACITY_KB=3\n")
	assert.ErrorContains(t, err, "SCRATCH_FSTYPE is missing")

	_, err = parseLocalNVMeFilesystemProbe("SCRATCH_ROOT_DEV=1\nSCRATCH_DEV=2\nSCRATCH_FSTYPE=ext2/ext3\nSCRATCH_CAPACITY_KB=lots\n")
	assert.ErrorContains(t, err, `SCRATCH_CAPACITY_KB="lots" is not a number`)
}

func TestLocalNVMeFilesystemProbe(t *testing.T) {
	command := `test() { return 0; }
stat() {
    case "$*" in
        "-c %d /") echo 10;;
        "-c %d -- /scratch") echo 20;;
        "-f -c %T -- /scratch") echo ext2/ext3;;
        *) echo "unexpected stat $*" >&2; return 1;;
    esac
}
df() {
    [ "$*" = "-P -k -- /scratch" ] || { echo "unexpected df $*" >&2; return 1; }
    printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n'
    printf '/dev/md127 14883055296 1 2 1%% /scratch\n'
}
set -- /scratch
` + localNVMeFilesystemProbe()
	output, err := exec.CommandContext(t.Context(), "bash", "-c", command).CombinedOutput()
	require.NoError(t, err, string(output))

	filesystem, err := parseLocalNVMeFilesystemProbe(string(output))
	require.NoError(t, err)
	assert.Equal(t, localNVMeFilesystem{RootDevice: "10", ScratchDevice: "20", FilesystemType: "ext2/ext3", CapacityBytes: 14883055296 * 1024}, filesystem)
}

func TestLocalNVMeWriteProbe(t *testing.T) {
	scratch := t.TempDir()
	content := "worker-0@soperator-e2e-local-nvme-1"
	command := `sync() { [ "$1" = -- ]; }
set -- ` + framework.ShellJoin(scratch, "marker-dir", "marker", content) + `
` + localNVMeWriteProbe()
	output, err := exec.CommandContext(t.Context(), "bash", "-c", command).CombinedOutput()
	require.NoError(t, err, string(output))

	written, err := os.ReadFile(filepath.Join(scratch, "marker-dir", "marker"))
	require.NoError(t, err)
	assert.Equal(t, content, string(written))
	assert.NoError(t, expectReadback(string(output), localNVMeReadbackKey, content))
	assert.ErrorContains(t, expectReadback(string(output), localNVMeReadbackKey, "other"),
		`expected SCRATCH_READBACK "other", observed "`+content+`"`)
}

func TestLocalNVMePyxisProbe(t *testing.T) {
	scratch := t.TempDir()
	file := filepath.Join(scratch, "marker")
	require.NoError(t, os.WriteFile(file, []byte("expected"), 0o644))

	output, err := exec.CommandContext(t.Context(), "/bin/sh", "-ec", localNVMePyxisProbe(), "sh", file, "expected").CombinedOutput()
	require.NoError(t, err, string(output))
	assert.NoError(t, expectReadback(string(output), localNVMePyxisReadbackKey, "expected"))

	_, err = exec.CommandContext(t.Context(), "/bin/sh", "-ec", localNVMePyxisProbe(), "sh", file, "other").CombinedOutput()
	require.Error(t, err)
}

func TestLocalNVMeUserJobCommand(t *testing.T) {
	assert.Equal(t,
		`su - 'bob' -c 'timeout 180 srun --immediate=120 --nodes=1 --ntasks=1 --nodelist='"'"'worker-rack0-0'"'"' bash /scratch'`,
		localNVMeUserJobCommand("worker-rack0-0", 3*time.Minute, "bash /scratch"))
}

func TestSelectLocalNVMeNodeSets(t *testing.T) {
	nodeSet := func(name string, workers ...string) localNVMeNodeSet {
		out := localNVMeNodeSet{Name: name, MountPath: "/scratch"}
		for _, worker := range workers {
			out.Workers = append(out.Workers, localNVMeWorker{WorkerInfo: framework.WorkerInfo{Name: worker}})
		}
		return out
	}
	all := []localNVMeNodeSet{
		nodeSet("worker-a", "worker-a-0", "worker-a-1", "worker-a-2"),
		nodeSet("worker-b", "worker-b-0"),
		nodeSet("worker-c", "worker-c-0", "worker-c-1"),
	}
	allNames := localNVMeTestNames(all)

	selected, err := selectLocalNVMeNodeSets(all, 2, 2)
	require.NoError(t, err)
	require.Len(t, selected, 2)
	assert.Less(t, selected[0].Name, selected[1].Name, "selection is sorted by NodeSet name")
	for _, nodeSet := range selected {
		assert.LessOrEqual(t, len(nodeSet.Workers), 2)
		assert.Equal(t, min(2, len(allNames[nodeSet.Name])), len(nodeSet.Workers))
		assert.IsNonDecreasing(t, localNVMeWorkerNames(nodeSet.Workers), "workers are sorted by name")
		assert.Subset(t, allNames[nodeSet.Name], localNVMeWorkerNames(nodeSet.Workers))
	}

	selected, err = selectLocalNVMeNodeSets(all, 10, 10)
	require.NoError(t, err)
	assert.Equal(t, allNames, localNVMeTestNames(selected))

	_, err = selectLocalNVMeNodeSets(all, 0, 2)
	assert.ErrorContains(t, err, "limits must be positive")
}

func TestJoinLocalNVMeProblems(t *testing.T) {
	assert.NoError(t, joinLocalNVMeProblems("check", nil))

	sentinel := errors.New("sentinel")
	err := joinLocalNVMeProblems("check scratch", []error{
		fmt.Errorf("worker-a: %w", sentinel),
		errors.New("worker-b: late"),
	})
	require.Error(t, err)
	assert.Equal(t, "check scratch: worker-a: sentinel\nworker-b: late", err.Error())
	assert.ErrorIs(t, err, sentinel)
}

func TestLocalNVMeMarkerContent(t *testing.T) {
	assert.Equal(t, "worker-0@soperator-e2e-local-nvme-7", localNVMeMarkerContent("soperator-e2e-local-nvme-7", "worker-0"))
}
