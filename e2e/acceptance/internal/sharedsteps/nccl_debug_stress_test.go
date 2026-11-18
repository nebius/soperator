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

func TestNCCLDebugStressContainerLoop(t *testing.T) {
	for _, tc := range []struct {
		starts  int
		failure string
	}{{5, "none"}, {25, "none"}, {25, "start"}, {25, "marker"}} {
		t.Run(fmt.Sprintf("%d-%s", tc.starts, tc.failure), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "a 'quoted' directory")
			require.NoError(t, os.Mkdir(dir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "start-container"), nil, 0o600))
			stub := fmt.Sprintf(`hostname() { echo worker-0; }
timeout() { shift; "$@"; }
calls=0
srun() {
    calls=$((calls+1))
    [[ "$*" == *'--nccld-enabled=1'* && "$*" == *'--container-image='* ]] || return 99
    if [[ %s == start && "$calls" == 3 ]]; then echo 'missing foreign fstab' >&2; return 2; fi
    token="e2e-nccl:42:$calls"
    file=%s/log-$calls
    printf 'TOKEN=%%s\nLOG_PATH=%%s\n' "$token" "$file"
    if [[ %s != marker || "$calls" != 3 ]]; then printf '%%s\n' "$token" > "$file"; fi
}
export SLURM_JOB_ID=42
`, framework.ShellQuote(tc.failure), framework.ShellQuote(dir), framework.ShellQuote(tc.failure))
			output, err := exec.CommandContext(t.Context(), "bash", "-c", stub+ncclDebugContainerBatch(dir, tc.starts)).CombinedOutput()
			assert.Equal(t, tc.starts, strings.Count(string(output), "RESULT\t"), string(output))
			assert.FileExists(t, filepath.Join(dir, "42.done"))
			results, parseErr := parseNCCLDebugContainerResults(string(output), "worker-0", tc.starts)
			require.NoError(t, parseErr)
			scenario := &NCCLDebugStress{
				worker: framework.WorkerInfo{Name: "worker-0"}, startsPerJob: tc.starts,
				containerJobs: []framework.SbatchJob{{ID: "42"}}, outputs: map[string]string{"42": string(output)},
			}
			if tc.failure == "none" {
				require.NoError(t, err, string(output))
				require.NoError(t, scenario.checkContainerStarts(tc.starts))
				assert.Equal(t, tc.starts, results.markers)
			} else {
				require.Error(t, err)
				if tc.failure == "start" {
					require.ErrorContains(t, scenario.checkContainerStarts(tc.starts), "1/25 container starts failed")
				} else {
					require.NoError(t, scenario.checkContainerStarts(tc.starts))
					require.ErrorContains(t, scenario.checkLogMarkers(t.Context()), "verified 24/25 container log markers")
				}
			}
			require.Error(t, scenario.checkContainerStarts(tc.starts+1))
		})
	}
}

func TestNCCLDebugStressNativeLoop(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "start-native"), nil, 0o600))
	stub := `hostname() { echo worker-0; }
timeout() { shift; "$@"; }
calls=0
srun() {
    calls=$((calls+1))
    if ((calls == 3)); then touch "$directory/stop"; fi
}
export SLURM_JOB_ID=42
`
	output, err := exec.CommandContext(t.Context(), "bash", "-c", stub+ncclDebugNativeBatch(dir)).CombinedOutput()
	require.NoError(t, err, string(output))
	steps, err := validateNCCLDebugNativeResults(string(output), "worker-0")
	require.NoError(t, err)
	assert.Equal(t, 3, steps)
	assert.FileExists(t, filepath.Join(dir, "42.churning"))
}

func TestValidateNCCLDebugStressRejectsIncompleteEvidence(t *testing.T) {
	var results strings.Builder
	results.WriteString("BATCH_HOST=worker-0\n")
	for i := range 25 {
		fmt.Fprintf(&results, "RESULT\t%d\t0\t1\n", i)
	}
	valid := results.String()
	_, err := parseNCCLDebugContainerResults(valid, "worker-0", 25)
	require.NoError(t, err)
	for _, output := range []string{
		strings.ReplaceAll(valid, "worker-0", "worker-1"),
		strings.ReplaceAll(valid, "RESULT\t0\t0\t1\n", ""),
		valid + "RESULT\t0\t0\t1\n",
		valid + "RESULT\t25\t0\t1\n",
		strings.ReplaceAll(valid, "RESULT\t0\t0\t1", "RESULT\t0\tbad\t1"),
		strings.ReplaceAll(valid, "RESULT\t0\t0\t1", "RESULT\t0\t0\t2"),
		strings.ReplaceAll(valid, "RESULT\t0\t0\t1", "RESULT\t0\t0\t1\tgarbage"),
	} {
		_, err := parseNCCLDebugContainerResults(output, "worker-0", 25)
		require.Error(t, err, output)
	}
	for _, churn := range []string{"", "CHURN\t1\t0", "CHURN\t10\t1", "CHURN\t10\t0\nCHURN\t11\t0"} {
		_, err := validateNCCLDebugNativeResults("BATCH_HOST=worker-0\n"+churn, "worker-0")
		require.Error(t, err, churn)
	}
}
