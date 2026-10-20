package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
