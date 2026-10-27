package sharedsteps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestNCCLDebugBatchCommand(t *testing.T) {
	for _, level := range []string{"INFO", "WARN"} {
		t.Run(level, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "a 'quoted' directory")
			require.NoError(t, os.Mkdir(directory, 0o700))
			for _, name := range []string{"release-step", "release-job"} {
				require.NoError(t, os.WriteFile(filepath.Join(directory, name), nil, 0o600))
			}
			require.NoError(t, os.WriteFile(filepath.Join(directory, "step.sh"), []byte(ncclDebugTaskCommand(8, directory)), 0o600))
			// Execute both shell layers with a fake Slurm launcher and NCCL binary.
			// This exercises quoting and the gates without submitting a job.
			stub := fmt.Sprintf(`id() { if [ "$1" = -u ]; then echo 1000; else echo soperatorchecks; fi; }
hostname() { echo worker-0; }
all_reduce_perf() { printf 'NCCL_PAYLOAD=%%s\n' "$*"; }
timeout() { shift; "$@"; }
export -f id hostname all_reduce_perf timeout
srun() {
    printf 'SRUN_ARGUMENTS=%%s\n' "$*"
    while [[ "$1" == --* ]]; do shift; done
    NCCL_DEBUG=%s NCCL_DEBUG_FILE=/opt/soperator-outputs/local/nccl_logs/worker-0.42.0.out SLURM_JOB_ID=42 SLURM_STEP_ID=0 "$@"
}
`, framework.ShellQuote(level))
			command := ncclDebugBatchCommand(level, directory)
			output, err := exec.CommandContext(t.Context(), "bash", "-c", stub+command).CombinedOutput()
			require.NoError(t, err, string(output))
			assert.Equal(t, "-g 8 -b 32M -e 32M -f 2 -w 1 -n 1", parseKeyValueLine(string(output), "NCCL_PAYLOAD"))
			assert.Contains(t, string(output), "--nccld-enabled=1 --nccld-log-level="+level)
			assert.Contains(t, string(output), "--nccld-out-file=1 --nccld-out-stdout=0")
			metadata, err := os.ReadFile(filepath.Join(directory, "task"))
			require.NoError(t, err)
			_, err = parseNCCLDebugTask(string(metadata), "42", level)
			require.NoError(t, err)
			assert.FileExists(t, filepath.Join(directory, "step-finished"))
		})
	}
}

func TestParseNCCLDebugTask(t *testing.T) {
	metadata := "HOST=worker-0\nJOB=42\nSTEP=0\nUSER=soperatorchecks\nUID=1000\nDEBUG=INFO\nDEBUG_FILE=/opt/soperator-outputs/local/nccl_logs/worker-0.42.0.out\nELAPSED_SECONDS=1\n"
	task, err := parseNCCLDebugTask(metadata, "42", "INFO")
	require.NoError(t, err)
	assert.Equal(t, "worker-0", task.host)
	assert.Equal(t, "0", task.step)

	for name, replacement := range map[string][2]string{
		"root UID":       {"UID=1000", "UID=0"},
		"other user":     {"USER=soperatorchecks", "USER=root"},
		"other job":      {"JOB=42", "JOB=43"},
		"batch step":     {"STEP=0", "STEP=batch"},
		"unsafe host":    {"HOST=worker-0", "HOST=../worker-0"},
		"wrong level":    {"DEBUG=INFO", "DEBUG=WARN"},
		"wrong log path": {"DEBUG_FILE=/opt/soperator-outputs/local/nccl_logs/worker-0.42.0.out", "DEBUG_FILE=/tmp/elsewhere.out"},
		"bad duration":   {"ELAPSED_SECONDS=1", "ELAPSED_SECONDS=invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseNCCLDebugTask(strings.ReplaceAll(metadata, replacement[0], replacement[1]), "42", "INFO")
			require.Error(t, err)
		})
	}
}

func TestParseNCCLDebugResources(t *testing.T) {
	resources, err := parseNCCLDebugResources("RESOURCE=worker-0.42.0.state\nNAMESPACE=present\nRESOURCE=worker-0.42.0.state\n")
	require.NoError(t, err)
	assert.True(t, resources.namespace)
	assert.Equal(t, []string{"worker-0.42.0.state"}, resources.files)
	resources, err = parseNCCLDebugResources("NAMESPACE=present\n")
	require.NoError(t, err)
	assert.True(t, resources.namespace)
	assert.Empty(t, resources.files)
	_, err = parseNCCLDebugResources("")
	require.ErrorContains(t, err, "missing namespace status")
}

func TestValidateNCCLDebugLog(t *testing.T) {
	for _, test := range []struct {
		name, level, output string
		valid               bool
	}{
		{"INFO diagnostics", "INFO", "worker:1:2 [0] NCCL INFO Bootstrap : Using eth0", true},
		{"empty INFO", "INFO", "", false},
		{"plugin message alone", "INFO", "SPANK | NCCL DEBUG [I]: Setting NCCL_DEBUG=INFO", false},
		{"quiet WARN", "WARN", "", true},
		{"WARN diagnostics", "WARN", "worker:1:2 [0] NCCL WARN Some warning", true},
		{"INFO leaks into WARN", "WARN", "worker:1:2 [0] NCCL INFO Bootstrap : Using eth0", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateNCCLDebugLog(test.level, test.output)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestNCCLDebugResourcesCommands(t *testing.T) {
	task := ncclDebugTask{host: "worker-0", job: "42", step: "0"}
	command := ncclDebugResourcesCommand(task)
	assert.Contains(t, command, "worker-0.42.0.*")
	assert.Contains(t, command, "30-nccl-debug-42-0.fstab")
	assert.Contains(t, command, "nsenter --mount='/tmp/42/.ns/mnt'")
	assert.NotContains(t, command, "-delete")
	for _, script := range []string{command, ncclDebugRemoveResourcesCommand("42")} {
		output, err := exec.CommandContext(t.Context(), "bash", "-n", "-c", script).CombinedOutput()
		require.NoError(t, err, string(output))
	}
	assert.Contains(t, ncclDebugRemoveResourcesCommand("42"), "*.42.*")
	assert.Contains(t, ncclDebugRemoveResourcesCommand("42"), "30-nccl-debug-42-*.fstab*")
}

func TestNCCLDebugRequiresObservedActiveResources(t *testing.T) {
	run := &ncclDebugJob{job: framework.SbatchJob{ID: "42"}, task: ncclDebugTask{host: "worker-0", job: "42", step: "0"}}
	s := &NCCLDebug{jobs: []*ncclDebugJob{run}}
	require.ErrorContains(t, s.checkResources(), "missing worker-0.42.0.state")
	run.active.files = []string{"worker-0.42.0.state", "worker-0.42.0.user-init.lock", "worker-0.42.0.task-init-privileged.lock", "worker-0.42.0.task-init.lock"}
	require.NoError(t, s.checkResources())
	run.active.files[0] = "worker-0.43.0.state"
	require.ErrorContains(t, s.checkResources(), "missing worker-0.42.0.state")
}
