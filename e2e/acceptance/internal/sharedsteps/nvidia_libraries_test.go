package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNVIDIALibrariesRequiredCapabilitiesIncludeNGX(t *testing.T) {
	assert.Contains(t, nvidiaLibrariesRequiredCapabilities, "ngx")
	assert.Equal(t, []string{"ngx"}, missingNVIDIADriverCapabilities([]string{"compute", "graphics", "utility", "video"}, nvidiaLibrariesRequiredCapabilities))
	assert.Empty(t, missingNVIDIADriverCapabilities([]string{"all"}, nvidiaLibrariesRequiredCapabilities))
}

func TestParseNVIDIAPodLibrariesSortsByPath(t *testing.T) {
	output := "Defaulted container \"slurmd\"\n" +
		"NVIDIA_POD_LIBRARY=2081720 /usr/lib/x86_64-linux-gnu/libnvidia-ml.so.580.65.06\r\n" +
		"NVIDIA_POD_LIBRARY=29983464 /usr/lib/x86_64-linux-gnu/libcuda.so.580.65.06\n"

	libraries, err := parseNVIDIAPodLibraries(output)
	require.NoError(t, err)
	assert.Equal(t, []nvidiaDriverLibrary{
		{path: "/usr/lib/x86_64-linux-gnu/libcuda.so.580.65.06", size: 29983464},
		{path: "/usr/lib/x86_64-linux-gnu/libnvidia-ml.so.580.65.06", size: 2081720},
	}, libraries)
	assert.Equal(t, "libcuda.so.580.65.06", libraries[0].name())
}

func TestParseNVIDIAPodLibrariesRejectsMalformedOutput(t *testing.T) {
	_, err := parseNVIDIAPodLibraries("nothing listed\n")
	require.Error(t, err)
	assert.ErrorContains(t, err, "find NVIDIA_POD_LIBRARY lines")

	_, err = parseNVIDIAPodLibraries("NVIDIA_POD_LIBRARY=/usr/lib/libcuda.so.1\n")
	require.Error(t, err)
	assert.ErrorContains(t, err, "want size and path")

	_, err = parseNVIDIAPodLibraries("NVIDIA_POD_LIBRARY=-1 /usr/lib/libcuda.so.1\n")
	require.Error(t, err)
	assert.ErrorContains(t, err, "stat pod library /usr/lib/libcuda.so.1")
}

func TestDescribeNVIDIALibraryListingDiff(t *testing.T) {
	reference := []nvidiaDriverLibrary{
		{path: "/lib/libcuda.so.1", size: 10},
		{path: "/lib/libnvidia-ml.so.1", size: 20},
		{path: "/lib/libnvidia-ngx.so.1", size: 30},
	}
	assert.Empty(t, describeNVIDIALibraryListingDiff(reference, reference))

	other := []nvidiaDriverLibrary{
		{path: "/lib/libcuda.so.1", size: 10},
		{path: "/lib/libnvidia-ml.so.1", size: 21},
		{path: "/lib/libnvoptix.so.1", size: 40},
	}
	diff := describeNVIDIALibraryListingDiff(reference, other)
	assert.Contains(t, diff, "only on the first: /lib/libnvidia-ngx.so.1")
	assert.Contains(t, diff, "only on the second: /lib/libnvoptix.so.1")
	assert.Contains(t, diff, "size differs: /lib/libnvidia-ml.so.1 (20 vs 21 bytes)")
}

func TestNVIDIALibrariesJobCommand(t *testing.T) {
	probe := "echo view"

	native, err := nvidiaLibrariesJobCommand("native", probe)
	require.NoError(t, err)
	assert.Equal(t, "srun bash -lc 'echo view'", native)

	enroot, err := nvidiaLibrariesJobCommand("Enroot", probe)
	require.NoError(t, err)
	assert.Equal(t, "NVIDIA_VISIBLE_DEVICES=all NVIDIA_DRIVER_CAPABILITIES=all srun "+
		"--container-image='docker://cr.nebius.cloud#soperator/ubuntu:noble' bash -lc 'echo view'", enroot)

	docker, err := nvidiaLibrariesJobCommand("Docker", probe)
	require.NoError(t, err)
	assert.Equal(t, "srun docker run --rm --name e2e-nvidia-libraries-${SLURM_JOB_ID}-${SLURM_NODEID} --gpus=all "+
		"-e SLURMD_NODENAME -e NVIDIA_DRIVER_CAPABILITIES=all 'cr.nebius.cloud/soperator/ubuntu:noble' bash -lc 'echo view'", docker)

	_, err = nvidiaLibrariesJobCommand("Podman", probe)
	require.Error(t, err)
	assert.ErrorContains(t, err, `unsupported runtime "Podman"`)
}

func TestNVIDIALibraryViewProbe(t *testing.T) {
	probe := nvidiaLibraryViewProbe([]string{"libcuda.so.580.65.06", "libnvidia-ngx.so.580.65.06"})
	assert.Contains(t, probe, `libdir="/usr/lib/$(uname -m)-linux-gnu"`)
	assert.Contains(t, probe, `for name in 'libcuda.so.580.65.06' 'libnvidia-ngx.so.580.65.06'; do`)
	assert.Contains(t, probe, `stem="${name%%.so*}.so"`)
	assert.Contains(t, probe, `stat -c %s "$file"`)
	assert.Contains(t, probe, `printf 'NVIDIA_LIBRARY_VIEW=%s %s %s %s %s %s\n'`)
}

func TestParseNVIDIALibraryViews(t *testing.T) {
	output := "NVIDIA_LIBRARY_VIEW=worker-0 libcuda.so.580.65.06 /usr/lib/x86_64-linux-gnu/libcuda.so.580.65.06 file 29983464 libcuda.so.1,libcuda.so\n" +
		"NVIDIA_LIBRARY_VIEW=worker-1 libnvidia-ngx.so.580.65.06 /usr/lib/x86_64-linux-gnu/libnvidia-ngx.so.580.65.06 missing 0 -\n"

	views, err := parseNVIDIALibraryViews(output)
	require.NoError(t, err)
	assert.Equal(t, []nvidiaLibraryView{
		{node: "worker-0", name: "libcuda.so.580.65.06", path: "/usr/lib/x86_64-linux-gnu/libcuda.so.580.65.06", kind: "file", size: 29983464, sonames: []string{"libcuda.so.1", "libcuda.so"}},
		{node: "worker-1", name: "libnvidia-ngx.so.580.65.06", path: "/usr/lib/x86_64-linux-gnu/libnvidia-ngx.so.580.65.06", kind: "missing"},
	}, views)

	_, err = parseNVIDIALibraryViews("NVIDIA_LIBRARY_VIEW=worker-0 libcuda.so.1 file\n")
	require.Error(t, err)
	assert.ErrorContains(t, err, "want node, library, path, kind, size and sonames")

	_, err = parseNVIDIALibraryViews("no views\n")
	require.Error(t, err)
	assert.ErrorContains(t, err, "find NVIDIA_LIBRARY_VIEW lines")
}

func TestValidateNVIDIALibraryViews(t *testing.T) {
	libraries := []nvidiaDriverLibrary{
		{path: "/usr/lib/x86_64-linux-gnu/libcuda.so.580.65.06", size: 100},
		{path: "/usr/lib/x86_64-linux-gnu/libnvidia-ngx.so.580.65.06", size: 200},
	}
	good := func(node string) []nvidiaLibraryView {
		return []nvidiaLibraryView{
			{node: node, name: "libcuda.so.580.65.06", path: "/usr/lib/x86_64-linux-gnu/libcuda.so.580.65.06", kind: "file", size: 100, sonames: []string{"libcuda.so.1"}},
			{node: node, name: "libnvidia-ngx.so.580.65.06", path: "/usr/lib/x86_64-linux-gnu/libnvidia-ngx.so.580.65.06", kind: "file", size: 200, sonames: []string{"libnvidia-ngx.so.1"}},
		}
	}
	nodes := []string{"worker-0", "worker-1"}
	assert.NoError(t, validateNVIDIALibraryViews(nodes, libraries, append(good("worker-0"), good("worker-1")...)))

	views := append(good("worker-0"), []nvidiaLibraryView{
		{node: "worker-1", name: "libcuda.so.580.65.06", path: "/usr/lib/x86_64-linux-gnu/libcuda.so.580.65.06", kind: "file", size: 25, sonames: []string{"libcuda.so.1"}},
	}...)
	err := validateNVIDIALibraryViews(nodes, libraries, views)
	require.Error(t, err)
	assert.ErrorContains(t, err, "check libcuda.so.580.65.06 on worker-1: want 100 bytes as in the worker pod, got 25")
	assert.ErrorContains(t, err, "check libnvidia-ngx.so.580.65.06 on worker-1: no view reported")
	assert.NotContains(t, err.Error(), "worker-0")

	views = []nvidiaLibraryView{
		{node: "worker-0", name: "libcuda.so.580.65.06", path: "/usr/lib/x86_64-linux-gnu/libcuda.so.580.65.06", kind: "symlink"},
		{node: "worker-0", name: "libnvidia-ngx.so.580.65.06", path: "/usr/lib/x86_64-linux-gnu/libnvidia-ngx.so.580.65.06", kind: "file", size: 200},
	}
	err = validateNVIDIALibraryViews([]string{"worker-0"}, libraries, views)
	require.Error(t, err)
	assert.ErrorContains(t, err, "want a regular file at /usr/lib/x86_64-linux-gnu/libcuda.so.580.65.06, got symlink")
	assert.ErrorContains(t, err, "no linker cache entry resolves to /usr/lib/x86_64-linux-gnu/libnvidia-ngx.so.580.65.06")
}
