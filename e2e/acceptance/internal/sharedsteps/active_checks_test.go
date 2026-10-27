package sharedsteps

import (
	"context"
	"strings"
	"testing"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type activeCleanupRuntime struct {
	framework.Runtime
	succeeded bool
	calls     []string
}

func (r *activeCleanupRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		r.calls = append(r.calls, strings.Join(args, " "))
		if args[0] == "get" {
			if r.succeeded {
				return `{"status":{"succeeded":1}}`, nil
			}
			return `{"status":{"failed":1}}`, nil
		}
		return "", nil
	})
}

func (r *activeCleanupRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		r.calls = append(r.calls, command)
		return "", nil
	})
}

func TestGPUActiveCheckCleanupStopsFailedProducerAndOwnedSlurmJobs(t *testing.T) {
	for _, succeeded := range []bool{false, true} {
		runtime := &activeCleanupRuntime{succeeded: succeeded}
		steps := NewActiveChecks(nil, runtime, framework.NewSlurmClient(runtime), framework.NewKubectlClient(runtime), nil)
		steps.gpuCheckK8sJobName = "owned-gpu-check"
		steps.gpuCheckJobIDs = []string{"123"}
		steps.CleanupAndReset(t.Context())
		if succeeded {
			assert.Len(t, runtime.calls, 1)
		} else {
			require.Len(t, runtime.calls, 3)
			assert.Contains(t, runtime.calls[1], "delete job -n soperator owned-gpu-check")
			assert.Equal(t, "scancel '123'", runtime.calls[2])
		}
		assert.Empty(t, steps.gpuCheckK8sJobName)
		assert.Empty(t, steps.gpuCheckJobIDs)
	}
}

func TestParseAnsibleDryRunJSONL(t *testing.T) {
	t.Parallel()

	output := `Mount jail
{"_event":"v2_runner_on_ok","task":{"name":"soperator_scripts : Add soperator_utils to PATH for all users"},"hosts":{"localhost":{"changed":true}}}
{"_event":"v2_runner_on_ok","task":{"name":"another task"},"hosts":{"localhost":{"changed":true}}}
{"_event":"v2_playbook_on_stats","stats":{"localhost":{"changed":7,"failures":0,"unreachable":0}}}
`

	result, err := parseAnsibleDryRunJSONL(output)
	require.NoError(t, err)
	assert.True(t, result.Parsed)
	assert.True(t, result.StatsFound)
	assert.True(t, result.ManagedFileChanged)
	assert.Zero(t, result.Failures)
	assert.Zero(t, result.Unreachable)
}

func TestParseAnsibleDryRunJSONLAggregatesFailures(t *testing.T) {
	t.Parallel()

	output := `{"_event":"v2_playbook_on_stats","stats":{"worker-0":{"failures":1,"unreachable":0},"worker-1":{"failures":0,"unreachable":2}}}`

	result, err := parseAnsibleDryRunJSONL(output)
	require.NoError(t, err)
	assert.True(t, result.StatsFound)
	assert.Equal(t, 1, result.Failures)
	assert.Equal(t, 2, result.Unreachable)
}

func TestParseAnsibleDryRunJSONLRejectsMalformedEvent(t *testing.T) {
	t.Parallel()

	_, err := parseAnsibleDryRunJSONL("preamble\n{not-json}\n")
	require.ErrorContains(t, err, "decode JSONL event on line 2")
}

func TestParseAnsibleDryRunJSONLRequiresEvents(t *testing.T) {
	t.Parallel()

	_, err := parseAnsibleDryRunJSONL("Mount jail\nRun Ansible\n")
	require.ErrorContains(t, err, "output does not contain JSONL events")
}
