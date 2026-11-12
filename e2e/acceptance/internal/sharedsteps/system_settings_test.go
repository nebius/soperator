package sharedsteps

import (
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestParseResourceLimitSnapshots(t *testing.T) {
	direct := computeResourceLimitProfile()
	nested := computeResourceLimitProfile()
	output := "cpu-bind: worker-0\n" + renderResourceLimitSnapshotsForTest(direct, nested)

	snapshots, err := parseResourceLimitSnapshots(output)
	require.NoError(t, err)
	assert.Equal(t, direct, snapshots.direct)
	assert.Equal(t, nested, snapshots.nested)
}

func TestParseResourceLimitSnapshotsRejectsMissingLimit(t *testing.T) {
	direct := computeResourceLimitProfile()
	delete(direct, limitOpenFiles)

	_, err := parseResourceLimitSnapshots(renderResourceLimitSnapshotsForTest(
		direct,
		computeResourceLimitProfile(),
	))
	require.Error(t, err)
	assert.ErrorContains(t, err, "find direct resource limit open_files")
}

func TestValidateLoginResourceLimitProfile(t *testing.T) {
	limits := loginResourceLimitProfile()
	limits[limitPendingSignals] = "514887"
	assert.NoError(t, validateResourceLimitProfile(limits, loginResourceLimitProfile(), true))

	limits[limitPendingSignals] = "0"
	err := validateResourceLimitProfile(limits, loginResourceLimitProfile(), true)
	require.Error(t, err)
	assert.ErrorContains(t, err, "want a positive integer")
}

func TestValidateComputeResourceLimitProfileReportsKnownDefects(t *testing.T) {
	limits := computeResourceLimitProfile()
	limits[limitOpenFiles] = "1024"
	limits[limitMaxMemory] = "10485760"

	err := validateResourceLimitProfile(limits, computeResourceLimitProfile(), false)
	require.Error(t, err)
	assert.ErrorContains(t, err, `open_files="1024": want "1048576"`)
	assert.ErrorContains(t, err, `max_memory="10485760": want "unlimited"`)
}

func TestValidateRegularUserComputeResourceLimitProfile(t *testing.T) {
	limits := computeResourceLimitProfile()
	limits[limitSchedulingPriority] = "40"
	assert.NoError(t, validateResourceLimitProfile(limits, regularUserComputeResourceLimitProfile(), false))

	err := validateResourceLimitProfile(computeResourceLimitProfile(), regularUserComputeResourceLimitProfile(), false)
	require.Error(t, err)
	assert.ErrorContains(t, err, `scheduling_priority="unlimited": want "40"`)
}

func TestSrunProbeCommand(t *testing.T) {
	tests := []struct {
		name      string
		container probeContainer
		want      string
	}{
		{
			name:      "native",
			container: probeContainerNone,
			want:      "srun --nodes=1 --ntasks=1 bash -lc 'ulimit -n'",
		},
		{
			name:      "enroot",
			container: probeContainerEnroot,
			want: "srun --nodes=1 --ntasks=1 --container-image='docker://cr.nebius.cloud#soperator/ubuntu:noble' " +
				"bash -lc 'ulimit -n'",
		},
		{
			name:      "docker",
			container: probeContainerDocker,
			want: "srun --nodes=1 --ntasks=1 docker run --rm --name e2e-system-settings-${SLURM_JOB_ID} " +
				"'cr.nebius.cloud/soperator/ubuntu:noble' bash -lc 'ulimit -n'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, srunProbeCommand("ulimit -n", tt.container))
		})
	}
}

func TestSrunProbeCommandAddsFlagsBeforeContainer(t *testing.T) {
	assert.Equal(t,
		"srun --nodes=1 --ntasks=1 --pty --container-image='docker://cr.nebius.cloud#soperator/ubuntu:noble' bash -lc 'ulimit -n'",
		srunProbeCommand("ulimit -n", probeContainerEnroot, "--pty"),
	)
}

func TestInteractiveProbeCommand(t *testing.T) {
	srun := "srun --nodes=1 --ntasks=1 --pty --immediate=120 --nodelist='worker-0' bash -lc 'ulimit -n'"
	assert.Equal(t,
		"su - 'bob' -c "+framework.ShellQuote("timeout 300 script -qec "+framework.ShellQuote(srun)+" /dev/null"),
		interactiveProbeCommand("bob", "worker-0", "ulimit -n", probeContainerNone),
	)
}

func TestPAMSlurmAdoptRequiresJob(t *testing.T) {
	tests := []struct {
		name    string
		cluster framework.SlurmClusterInfo
		groups  []string
		want    bool
	}{
		{name: "disabled", cluster: framework.SlurmClusterInfo{}, want: false},
		{name: "enabled without exemptions", cluster: framework.SlurmClusterInfo{PAMSlurmAdoptEnabled: true}, groups: []string{"bob"}, want: true},
		{
			name:    "exempt user",
			cluster: framework.SlurmClusterInfo{PAMSlurmAdoptEnabled: true, PAMSlurmAdoptExemptUsers: []string{"alice", "bob"}},
			want:    false,
		},
		{
			name:    "exempt group",
			cluster: framework.SlurmClusterInfo{PAMSlurmAdoptEnabled: true, PAMSlurmAdoptExemptGroups: []string{"sudo"}},
			groups:  []string{"bob", "sudo", "docker"},
			want:    false,
		},
		{
			name:    "other exemptions",
			cluster: framework.SlurmClusterInfo{PAMSlurmAdoptEnabled: true, PAMSlurmAdoptExemptUsers: []string{"alice"}, PAMSlurmAdoptExemptGroups: []string{"admins"}},
			groups:  []string{"bob", "sudo"},
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pamSlurmAdoptRequiresJob(tt.cluster, "bob", tt.groups))
		})
	}
}

func TestParseCombinedResourceLimitsAndKernelSettingsFromPTY(t *testing.T) {
	limits := renderResourceLimitSnapshotsForTest(computeResourceLimitProfile(), computeResourceLimitProfile())
	kernel := `fs.file-max=9223372036854775807
vm.max_map_count=655300
kernel.unprivileged_userns_clone=1
net.core.rmem_max=536870912
net.core.wmem_max=536870912
net.ipv4.tcp_rmem=4096	131072	536870912
net.ipv4.tcp_wmem=4096	16384	536870912
`
	output := strings.ReplaceAll(limits+kernel, "\n", "\r\n")

	snapshots, err := parseResourceLimitSnapshots(output)
	require.NoError(t, err)
	assert.Equal(t, computeResourceLimitProfile(), snapshots.direct)
	settings, err := parseKernelSettings(output)
	require.NoError(t, err)
	assert.NoError(t, validateKernelSettings("test", settings))
}

func TestValidateRegularUserSlurmResourceLimitSnapshots(t *testing.T) {
	limits := regularUserComputeResourceLimitProfile()
	limits[limitMaxMemory] = "10485760"
	snapshots := resourceLimitSnapshots{direct: limits, nested: maps.Clone(limits)}
	assert.NoError(t, validateSlurmResourceLimitSnapshots(snapshots, regularUserComputeResourceLimitProfile()))

	err := validateSlurmResourceLimitSnapshots(snapshots, computeResourceLimitProfile())
	require.Error(t, err)
	assert.ErrorContains(t, err, `scheduling_priority="40": want "unlimited"`)
}

func TestSrunProbeCommandForGPUContainers(t *testing.T) {
	assert.Equal(t,
		"NVIDIA_VISIBLE_DEVICES=all NVIDIA_DRIVER_CAPABILITIES=compute,graphics,utility,video srun --nodes=1 --ntasks=1 "+
			"--container-image='docker://cr.nebius.cloud#soperator/ubuntu:noble' bash -lc 'true'",
		srunProbeCommand("true", probeContainerEnrootGPU),
	)
	assert.Equal(t,
		"srun --nodes=1 --ntasks=1 docker run --rm --name e2e-system-settings-${SLURM_JOB_ID} "+
			"--gpus=all -e NVIDIA_DRIVER_CAPABILITIES=compute,graphics,utility,video "+
			"'cr.nebius.cloud/soperator/ubuntu:noble' bash -lc 'true'",
		srunProbeCommand("true", probeContainerDockerGPU),
	)
}

func TestProbeContainerIsDocker(t *testing.T) {
	assert.True(t, probeContainerDocker.isDocker())
	assert.True(t, probeContainerDockerGPU.isDocker())
	assert.False(t, probeContainerNone.isDocker())
	assert.False(t, probeContainerEnrootGPU.isDocker())
}

func TestRequiredNVIDIADriverCapabilities(t *testing.T) {
	assert.Equal(t, []string{"compute", "graphics", "utility", "video"}, requiredNVIDIADriverCapabilities())
}

func TestNVIDIALibraryProbeLooksUpEveryCapabilityLibrary(t *testing.T) {
	probe := nvidiaLibraryProbe()
	for _, entry := range nvidiaCapabilityLibraries {
		for _, library := range entry.libraries {
			assert.Contains(t, probe, entry.capability+":"+library)
		}
	}
	assert.Contains(t, probe, `printf 'NVIDIA_LIBRARY=%s %s %s %s\n'`)
	assert.Contains(t, probe, `capability="${entry%%:*}"`)
	assert.Contains(t, probe, `stat -Lc %s "$path"`)
}

func TestParseNVIDIALibraryLookups(t *testing.T) {
	output := "cpu-bind: worker-0\r\n" +
		"NVIDIA_LIBRARY=compute libcuda.so.1 /usr/lib/x86_64-linux-gnu/libcuda.so.1 29983464\r\n" +
		"NVIDIA_LIBRARY=graphics libEGL_nvidia.so.0 - 0\n"

	lookups, err := parseNVIDIALibraryLookups(output)
	require.NoError(t, err)
	assert.Equal(t, []nvidiaLibraryLookup{
		{capability: "compute", library: "libcuda.so.1", path: "/usr/lib/x86_64-linux-gnu/libcuda.so.1", size: 29983464},
		{capability: "graphics", library: "libEGL_nvidia.so.0"},
	}, lookups)
}

func TestParseNVIDIALibraryLookupsRejectsMalformedOutput(t *testing.T) {
	_, err := parseNVIDIALibraryLookups("no lookups\n")
	require.Error(t, err)
	assert.ErrorContains(t, err, "find NVIDIA_LIBRARY lines")

	_, err = parseNVIDIALibraryLookups("NVIDIA_LIBRARY=compute libcuda.so.1\n")
	require.Error(t, err)
	assert.ErrorContains(t, err, "want capability, library, path and size")

	_, err = parseNVIDIALibraryLookups("NVIDIA_LIBRARY=compute libcuda.so.1 /lib/libcuda.so.1 big\n")
	require.Error(t, err)
	assert.ErrorContains(t, err, "parse size")
}

func TestValidateNVIDIALibraryLookups(t *testing.T) {
	lookups := []nvidiaLibraryLookup{
		{capability: "compute", library: "libcuda.so.1", path: "/lib/libcuda.so.1", size: 29983464},
		// One non-placeholder library is enough for a capability.
		{capability: "graphics", library: "libEGL_nvidia.so.0"},
		{capability: "graphics", library: "libGLX_nvidia.so.0", path: "/lib/libGLX_nvidia.so.0", size: 1047528},
		{capability: "utility", library: "libnvidia-ml.so.1", path: "/lib/libnvidia-ml.so.1", size: 2081720},
		{capability: "video", library: "libnvcuvid.so.1", path: "/lib/libnvcuvid.so.1", size: 13148544},
		{capability: "video", library: "libnvidia-encode.so.1", path: "/lib/libnvidia-encode.so.1", size: 249000},
	}
	assert.NoError(t, validateNVIDIALibraryLookups("native job", lookups))
}

func TestValidateNVIDIALibraryLookupsRejectsPlaceholdersAndMissingLibraries(t *testing.T) {
	lookups := []nvidiaLibraryLookup{
		{capability: "compute", library: "libcuda.so.1", path: "/lib/libcuda.so.1", size: 29983464},
		{capability: "graphics", library: "libEGL_nvidia.so.0", path: "/lib/libEGL_nvidia.so.0", size: 64},
		{capability: "graphics", library: "libGLX_nvidia.so.0"},
		{capability: "utility", library: "libnvidia-ml.so.1", path: "/lib/libnvidia-ml.so.1", size: 2081720},
	}

	err := validateNVIDIALibraryLookups("native job", lookups)
	require.Error(t, err)
	assert.ErrorContains(t, err,
		"check native job NVIDIA graphics capability: want a non-placeholder library of at least 1024 bytes, "+
			"got libEGL_nvidia.so.0=/lib/libEGL_nvidia.so.0 (64 bytes), libGLX_nvidia.so.0 not found")
	assert.ErrorContains(t, err, "NVIDIA video capability: want a non-placeholder library of at least 1024 bytes, got no lookups")
	assert.NotContains(t, err.Error(), "compute capability")
}

func TestSystemSettingsDockerContainerName(t *testing.T) {
	assert.Equal(t, "e2e-system-settings-42", systemSettingsDockerContainerName("42"))
}

func TestJobUserProbe(t *testing.T) {
	assert.Equal(t, `printf 'JOB_USER=%s\n' "$(id -un)"`, jobUserProbe())
}

func TestValidateJobUser(t *testing.T) {
	output := "cpu-bind: worker-0\nJOB_USER=bob\n" + renderResourceLimitSnapshotsForTest(
		computeResourceLimitProfile(),
		computeResourceLimitProfile(),
	)
	assert.NoError(t, validateJobUser(output, "bob"))

	err := validateJobUser("JOB_USER=root\n", "bob")
	require.Error(t, err)
	assert.ErrorContains(t, err, `task user "root": want "bob"`)

	err = validateJobUser("no user line\n", "bob")
	require.Error(t, err)
	assert.ErrorContains(t, err, `task user "": want "bob"`)
}

func TestCompareResourceLimitSnapshots(t *testing.T) {
	direct := computeResourceLimitProfile()
	nested := computeResourceLimitProfile()
	assert.NoError(t, compareResourceLimitSnapshots(direct, nested))

	nested[limitMaxMemory] = "1505755136"
	err := compareResourceLimitSnapshots(direct, nested)
	require.Error(t, err)
	assert.ErrorContains(t, err, "nested Bash resource limit max_memory")
}

func TestValidateResourceLimitSnapshotsAggregatesProfileAndNestedFailures(t *testing.T) {
	direct := computeResourceLimitProfile()
	direct[limitOpenFiles] = "1024"
	nested := computeResourceLimitProfile()
	nested[limitOpenFiles] = "1024"
	nested[limitMaxMemory] = "1505755136"

	err := validateResourceLimitSnapshots(resourceLimitSnapshots{direct: direct, nested: nested}, computeResourceLimitProfile(), false)
	require.Error(t, err)
	assert.ErrorContains(t, err, "validate direct resource limits")
	assert.ErrorContains(t, err, "validate nested Bash resource limits")
	assert.ErrorContains(t, err, "compare direct and nested Bash resource limits")
}

func TestValidateSlurmResourceLimitSnapshots(t *testing.T) {
	direct := computeResourceLimitProfile()
	direct[limitMaxMemory] = "10485760"
	nested := computeResourceLimitProfile()
	nested[limitMaxMemory] = "10485760"

	assert.NoError(t, validateSlurmResourceLimitSnapshots(resourceLimitSnapshots{direct: direct, nested: nested}, computeResourceLimitProfile()))
}

func TestValidateSlurmResourceLimitSnapshotsRejectsInvalidMaxMemory(t *testing.T) {
	direct := computeResourceLimitProfile()
	direct[limitMaxMemory] = "unlimited"
	nested := computeResourceLimitProfile()
	nested[limitMaxMemory] = "10485760"

	err := validateSlurmResourceLimitSnapshots(resourceLimitSnapshots{direct: direct, nested: nested}, computeResourceLimitProfile())
	require.Error(t, err)
	assert.ErrorContains(t, err, "want a positive integer set from Slurm job memory")
	assert.ErrorContains(t, err, "compare direct and nested Bash resource limits")
}

func TestParseAndValidateKernelSettings(t *testing.T) {
	output := `cpu-bind: worker-0
fs.file-max=9223372036854775807
vm.max_map_count=655300
kernel.unprivileged_userns_clone=1
net.core.rmem_max=536870912
net.core.wmem_max=536870912
net.ipv4.tcp_rmem=4096	131072	536870912
net.ipv4.tcp_wmem=4096 16384 536870912
`

	settings, err := parseKernelSettings(output)
	require.NoError(t, err)
	assert.Equal(t, "4096 131072 536870912", settings[kernelTCPReceiveBufferSize])
	assert.NoError(t, validateKernelSettings("test", settings))
}

func TestValidateKernelSettingsRejectsValuesBelowMinimum(t *testing.T) {
	settings := kernelSettingSnapshot{
		kernelFileMax:              "388066",
		kernelMaxMapCount:          "65529",
		kernelUnprivilegedUserNS:   "1",
		kernelReceiveBufferMax:     "536870912",
		kernelSendBufferMax:        "536870912",
		kernelTCPReceiveBufferSize: "4096 131072 536870912",
		kernelTCPSendBufferSize:    "4096 16384 536870912",
	}

	err := validateKernelSettings("test", settings)
	require.Error(t, err)
	assert.ErrorContains(t, err, "fs.file-max")
	assert.ErrorContains(t, err, "vm.max_map_count")
}

func TestValidateNVIDIADriverCapabilities(t *testing.T) {
	assert.NoError(t, validateNVIDIADriverCapabilities("video,utility,compute,graphics,display"))

	err := validateNVIDIADriverCapabilities("compute,utility")
	require.Error(t, err)
	assert.ErrorContains(t, err, "graphics,video")
}

func renderResourceLimitSnapshotsForTest(direct, nested resourceLimitSnapshot) string {
	output := limitDirectMarker + "\n"
	for _, limit := range resourceLimitOptions {
		if value, found := direct[limit.name]; found {
			output += limit.name + "=" + value + "\n"
		}
	}
	output += limitNestedMarker + "\n"
	for _, limit := range resourceLimitOptions {
		if value, found := nested[limit.name]; found {
			output += limit.name + "=" + value + "\n"
		}
	}
	return output
}
