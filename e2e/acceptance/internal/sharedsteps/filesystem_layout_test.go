package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

func TestFilesystemLayoutMounts(t *testing.T) {
	template := json.RawMessage(`{"resources":{"requests":{"storage":"256Gi"}}}`)
	var config = []kubeobjects.NodeVolumeMount{
		{Name: "home-dir", MountPath: "/users"},
		{Name: "data", MountPath: "/shared"},
		{Name: "image-storage", MountPath: "/images"},
		{Name: "scratch", MountPath: "/scratch", VolumeClaimTemplateSpec: &template},
		{Name: "unrelated", MountPath: "/etc/config", ReadOnly: true},
	}
	mounts, err := filesystemLayoutMounts(config, true)
	require.NoError(t, err)
	require.Len(t, mounts, 8)
	assert.Equal(t, "/users", mounts[3].Path)
	assert.Equal(t, "/shared", mounts[4].Path)
	assert.Equal(t, "NFS home", mounts[3].Mount)
	assert.Equal(t, "Image storage", mounts[5].Mount)

	loginConfig := append([]kubeobjects.NodeVolumeMount(nil), config[:2]...)
	loginConfig = append(loginConfig, kubeobjects.NodeVolumeMount{Name: "login-image-storage", MountPath: "/images", VolumeClaimTemplateSpec: &template})
	loginMounts, err := filesystemLayoutMounts(loginConfig, false)
	require.NoError(t, err)
	require.Len(t, loginMounts, 6)
	assert.Equal(t, filesystemLayoutMount{Mount: "Image storage", Name: "login-image-storage", Path: "/images"}, loginMounts[5])
	loginConfig[0].Name = "home"
	loginMounts, err = filesystemLayoutMounts(loginConfig, false)
	require.NoError(t, err)
	assert.Equal(t, "NFS home", loginMounts[3].Mount)
	config[3] = kubeobjects.NodeVolumeMount{Name: framework.LocalNVMeVolumeName, MountPath: "/mnt/local-nvme"}
	mounts, err = filesystemLayoutMounts(config, true)
	require.NoError(t, err)
	assert.Equal(t, "Scratch", mounts[6].Mount)
	assert.Equal(t, "/mnt/local-nvme", mounts[6].Path)
	config[0].ReadOnly = true
	_, err = filesystemLayoutMounts(config, true)
	require.ErrorContains(t, err, "read-only")
	config[0].ReadOnly, config[0].MountPath = false, "relative"
	_, err = filesystemLayoutMounts(config, true)
	require.Error(t, err)
}

func TestParseFilesystemLayoutProbe(t *testing.T) {
	const valid = `{"filesystems":[{"target":"/mnt/jail/home","source":"nfs:/export","fstype":"nfs4","size":1000,"maj:min":"0:42"}]}
2755
`
	probe, err := parseFilesystemLayoutProbe(valid, "/mnt/jail/home")
	require.NoError(t, err)
	assert.Equal(t, "2755", probe.Mode)
	assert.Equal(t, json.Number("1000"), probe.Size)
	_, err = parseFilesystemLayoutProbe(strings.Replace(valid, `"size":1000`, `"size":"1000"`, 1), "/mnt/jail/home")
	require.NoError(t, err)
	for _, out := range []string{
		`{"filesystems":[]}`, "invalid", strings.Replace(valid, "1000", "0", 1),
		strings.Replace(valid, "/mnt/jail/home", "/mnt/jail", 1),
		strings.Replace(valid, "nfs:/export", "", 1),
	} {
		_, err := parseFilesystemLayoutProbe(out, "/mnt/jail/home")
		require.Error(t, err)
	}
}

func TestFilesystemLayoutCapacity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     string
		atLeast  bool
		wantFail bool
	}{
		{"exact", "1000", false, false},
		{"filesystem overhead", "980", false, false},
		{"too small", "500", false, true},
		{"wrong larger disk", "2000", false, true},
		{"NVMe exceeds quota", "2000", true, false},
		{"invalid", "bad", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFilesystemLayoutCapacity(filesystemLayoutProbe{Size: json.Number(tc.size)}, 1000, tc.atLeast)
			assert.Equal(t, tc.wantFail, err != nil)
		})
	}
}

func TestFilesystemLayoutRejectsBrokenConfiguredMount(t *testing.T) {
	s := &FilesystemLayout{}
	mount := filesystemLayoutMount{Name: "data", Type: "virtiofs", Mode: "777", Source: &corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "configured-data"},
	}}
	probe := filesystemLayoutProbe{Type: "virtiofs", Mode: "777"}
	require.ErrorContains(t, s.checkMountCapacity(t.Context(), filesystemLayoutNode{}, mount, probe), "absent")
	node := filesystemLayoutNode{Pod: corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "wrong-data"},
	}}}}}}
	require.ErrorContains(t, s.checkMountCapacity(t.Context(), node, mount, probe), "configured claim")
	node.Pod.Spec.Volumes[0] = corev1.Volume{Name: "in-memory", VolumeSource: corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: resource.NewQuantity(1000, resource.BinarySI)},
	}}
	probe.Type, probe.Size = "tmpfs", "1000"
	require.NoError(t, s.checkMountCapacity(t.Context(), node, filesystemLayoutMount{Name: "in-memory", Type: "tmpfs", Mode: "777"}, probe))
}

type filesystemLayoutRuntime struct {
	framework.Runtime
	kubectl framework.ArgsScope
}

func (r filesystemLayoutRuntime) Kubectl() framework.ArgsScope { return r.kubectl }
func (r filesystemLayoutRuntime) Logf(string, ...any)          {}

func TestFilesystemLayoutNFSUsesServerCapacity(t *testing.T) {
	var calls []string
	runtime := filesystemLayoutRuntime{kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return `{"spec":{"resources":{"requests":{"storage":"930Gi"}}},"status":{"phase":"Bound"}}`, nil
	})}
	s := NewFilesystemLayout(nil, runtime, framework.NewKubectlClient(runtime), nil)
	node := filesystemLayoutNode{Pod: corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "home", VolumeSource: corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "home"},
	}}}}}}
	mount := filesystemLayoutMount{Name: "home", Type: "nfs4", Mode: "2755"}
	probe := filesystemLayoutProbe{Type: "nfs4", Mode: "2755", Size: json.Number(fmt.Sprint(930 * int64(1024*1024*1024)))}
	require.NoError(t, s.checkMountCapacity(t.Context(), node, mount, probe))
	require.Equal(t, []string{"get pvc nfs-server-storage -n nfs-system -o json"}, calls)
}

func TestFilesystemLayoutCleansPartiallyCreatedMarker(t *testing.T) {
	failure := errors.New("write failed")
	var commands []string
	runtime := filesystemLayoutRuntime{kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		commands = append(commands, args[len(args)-1])
		if len(commands) == 1 {
			return "", failure
		}
		return "", nil
	})}
	s := NewFilesystemLayout(nil, runtime, framework.NewKubectlClient(runtime), nil)
	const dir = "/mnt/jail/data/soperator-e2e-filesystem-owned"
	require.ErrorIs(t, s.writeMarker(t.Context(), filesystemLayoutNode{}, dir), failure)
	s.CleanupAndReset(t.Context())
	require.Len(t, commands, 2)
	assert.Equal(t, "rm -rf -- "+framework.ShellQuote(dir), commands[1])
	assert.Empty(t, s.markers)
	s.CleanupAndReset(t.Context())
	assert.Len(t, commands, 2, "cleanup is idempotent")
}

func TestParseFilesystemLayoutTable(t *testing.T) {
	header := []string{"Mount", "Path", "Filesystem", "Mode"}
	expectations, err := parseFilesystemLayoutTable(newTable(header, []string{" Memory ", " /mnt/memory ", " tmpfs ", " 1777 "}))
	require.NoError(t, err)
	assert.Equal(t, []filesystemLayoutExpectation{{Mount: "Memory", Path: "/mnt/memory", Type: "tmpfs", Mode: "1777"}}, expectations)
	for name, table := range map[string]*godog.Table{
		"nil": nil, "empty": {}, "header only": newTable(header),
		"wrong header":   newTable([]string{"Mount", "Path", "Type", "Mode"}, []string{"Memory", "/mnt/memory", "tmpfs", "1777"}),
		"missing column": newTable(header, []string{"Memory", "/mnt/memory", "tmpfs"}),
		"empty value":    newTable(header, []string{"Memory", "", "tmpfs", "1777"}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseFilesystemLayoutTable(table)
			require.Error(t, err)
		})
	}
}

func TestFilesystemLayoutChecksGherkinExpectationsOnApplicableNodes(t *testing.T) {
	template := json.RawMessage(`{}`)
	var config = []kubeobjects.NodeVolumeMount{
		{Name: "home-dir", MountPath: "/users"},
		{Name: "data", MountPath: "/shared"},
		{Name: "image-storage", MountPath: "/images", VolumeClaimTemplateSpec: &template},
		{Name: "scratch", MountPath: "/scratch", VolumeClaimTemplateSpec: &template},
		{Name: framework.LocalNVMeVolumeName, MountPath: "/mnt/local-nvme"},
	}
	for _, tc := range []struct {
		name, homeType, homeMode, wantError string
		missingLoginImage                   bool
		brokenImageOnPod                    string
		missingWorkerImage                  bool
		missingWorkerScratch                bool
	}{
		{name: "both nodes and worker-only mounts", homeType: "nfs4", homeMode: "2500"},
		{name: "mode comes from Gherkin", homeType: "nfs4", homeMode: "2755", wantError: "expected type=nfs4 mode=2755"},
		{name: "type comes from Gherkin", homeType: "ext4", homeMode: "2500", wantError: "expected type=ext4"},
		{name: "unconfigured login image storage is optional", homeType: "nfs4", homeMode: "2500", missingLoginImage: true},
		{name: "broken configured login image storage fails", homeType: "nfs4", homeMode: "2500", brokenImageOnPod: "login-0", wantError: "probe mount /mnt/jail/images on login-0"},
		{name: "unconfigured worker image storage is optional", homeType: "nfs4", homeMode: "2500", missingWorkerImage: true},
		{name: "broken configured worker image storage fails", homeType: "nfs4", homeMode: "2500", brokenImageOnPod: "worker-0", wantError: "probe mount /mnt/jail/images on worker-0"},
		{name: "missing worker storage fails", homeType: "nfs4", homeMode: "2500", missingWorkerScratch: true, wantError: "Scratch is not configured on pod worker-0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			runtime := filesystemLayoutRuntime{kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
				command := args[len(args)-1]
				if args[3] == tc.brokenImageOnPod && strings.Contains(command, framework.ShellQuote("/mnt/jail/images")) {
					return "", errors.New("mount not found")
				}
				probes := map[string]struct{ fsType, mode string }{
					"/mnt/jail/users":             {"nfs4", "2500"},
					"/mnt/jail/images":            {"ext4", "755"},
					"/mnt/jail/scratch":           {"ext4", "777"},
					"/mnt/jail/mnt/local-nvme":    {"ext4", "777"},
					"/mnt/jail/run/nvidia/driver": {"xfs", "755"},
					"/mnt/jail/test-tmp":          {"xfs", "777"},
				}
				for target, probe := range probes {
					if strings.Contains(command, framework.ShellQuote(target)) {
						calls = append(calls, args[3]+":"+target)
						return fmt.Sprintf(`{"filesystems":[{"target":%q,"source":"fixture","fstype":%q,"size":1000,"maj:min":"0:42"}]}`+"\n%s\n", target, probe.fsType, probe.mode), nil
					}
				}
				return "", fmt.Errorf("unexpected probe: %s", command)
			})}
			s := NewFilesystemLayout(nil, runtime, framework.NewKubectlClient(runtime), nil)
			loginConfig := config[:3]
			if tc.missingLoginImage {
				loginConfig = config[:2]
			}
			loginMounts, err := filesystemLayoutMounts(loginConfig, false)
			require.NoError(t, err)
			workerConfig := config
			if tc.missingWorkerImage {
				workerConfig = append(append([]kubeobjects.NodeVolumeMount(nil), config[:2]...), config[3:]...)
			}
			if tc.missingWorkerScratch {
				workerConfig = config[:3]
			}
			workerMounts, err := filesystemLayoutMounts(workerConfig, true)
			require.NoError(t, err)
			s.nodes = []filesystemLayoutNode{
				{Pod: corev1.Pod{}, Container: loginSSHDContainer, Mounts: loginMounts},
				{Pod: corev1.Pod{}, Container: slurmdContainerName, Mounts: workerMounts},
			}
			s.nodes[0].Pod.Name, s.nodes[1].Pod.Name = "login-0", "worker-0"
			table := newTable(
				[]string{"Mount", "Path", "Filesystem", "Mode"},
				[]string{"NFS home", "configured", tc.homeType, tc.homeMode},
				[]string{"Image storage", "configured", "ext4", "755"},
				[]string{"Scratch", "configured", "ext4", "777"},
				[]string{"NVIDIA driver", "/run/nvidia/driver", "configured", "755"},
				[]string{"Temporary storage", "/test-tmp", "configured", "777"},
			)
			err = s.checkMounts(t.Context(), table)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			var wantCalls = []string{
				"login-0:/mnt/jail/users", "login-0:/mnt/jail/test-tmp",
				"worker-0:/mnt/jail/users", "worker-0:/mnt/jail/scratch",
				"worker-0:/mnt/jail/mnt/local-nvme", "worker-0:/mnt/jail/run/nvidia/driver", "worker-0:/mnt/jail/test-tmp",
			}
			wantLoginMounts := 2
			if !tc.missingLoginImage {
				wantCalls = append(wantCalls, "login-0:/mnt/jail/images")
				wantLoginMounts++
			}
			wantWorkerMounts := 5
			if !tc.missingWorkerImage {
				wantCalls = append(wantCalls, "worker-0:/mnt/jail/images")
				wantWorkerMounts++
			}
			assert.ElementsMatch(t, wantCalls, calls)
			assert.Len(t, s.nodes[0].Mounts, wantLoginMounts)
			assert.Len(t, s.nodes[1].Mounts, wantWorkerMounts)
			assert.Equal(t, json.Number("1000"), s.nodes[0].Mounts[0].Observed.Size)
		})
	}
}
