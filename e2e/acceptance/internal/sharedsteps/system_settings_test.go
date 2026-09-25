package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
