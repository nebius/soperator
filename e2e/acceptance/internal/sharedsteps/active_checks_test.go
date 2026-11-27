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
	assert.Equal(t, 7, result.Changed)
	assert.Equal(t, []string{"soperator_scripts : Add soperator_utils to PATH for all users", "another task"}, result.ChangedTasks)
	assert.Zero(t, result.Failures)
	assert.Zero(t, result.Unreachable)
}

func TestParseAnsibleDryRunJSONLAggregatesHostStatistics(t *testing.T) {
	t.Parallel()

	output := `{"_event":"v2_playbook_on_stats","stats":{"worker-0":{"changed":2,"failures":1,"unreachable":0},"worker-1":{"changed":5,"failures":0,"unreachable":2}}}`

	result, err := parseAnsibleDryRunJSONL(output)
	require.NoError(t, err)
	assert.True(t, result.StatsFound)
	assert.Equal(t, 7, result.Changed)
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

func TestParseAnsibleDryRunJSONLRejectsIncompleteRecap(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		output string
		want   string
	}{
		{"missing recap", `{"_event":"v2_playbook_on_start"}`, "does not contain v2_playbook_on_stats"},
		{"missing hosts", `{"_event":"v2_playbook_on_stats"}`, "no host statistics"},
		{"empty hosts", `{"_event":"v2_playbook_on_stats","stats":{}}`, "no host statistics"},
		{"missing changed", `{"_event":"v2_playbook_on_stats","stats":{"localhost":{"failures":0,"unreachable":0}}}`, "missing changed, failures or unreachable"},
		{"missing failures", `{"_event":"v2_playbook_on_stats","stats":{"localhost":{"changed":0,"unreachable":0}}}`, "missing changed, failures or unreachable"},
		{"missing unreachable", `{"_event":"v2_playbook_on_stats","stats":{"localhost":{"changed":0,"failures":0}}}`, "missing changed, failures or unreachable"},
		{"null counter", `{"_event":"v2_playbook_on_stats","stats":{"localhost":{"changed":null,"failures":0,"unreachable":0}}}`, "missing changed, failures or unreachable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAnsibleDryRunJSONL(tt.output)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestJailDriftAssertions(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "clean jail",
			output: `{"_event":"v2_playbook_on_stats","stats":{"localhost":{"changed":0,"failures":0,"unreachable":0}}}`,
		},
		{
			name: "unrelated task changed",
			output: `{"_event":"v2_runner_on_ok","task":{"name":"nccl_tests : Install binaries"},"hosts":{"localhost":{"changed":true}}}
{"_event":"v2_playbook_on_stats","stats":{"localhost":{"changed":1,"failures":0,"unreachable":0}}}`,
			want: "changed=1 failures=0 unreachable=0; changed tasks=[nccl_tests : Install binaries]",
		},
		{
			name:   "failed host",
			output: `{"_event":"v2_playbook_on_stats","stats":{"localhost":{"changed":0,"failures":1,"unreachable":0}}}`,
			want:   "failures=1 unreachable=0",
		},
		{
			name:   "unreachable host",
			output: `{"_event":"v2_playbook_on_stats","stats":{"localhost":{"changed":0,"failures":0,"unreachable":1}}}`,
			want:   "failures=0 unreachable=1",
		},
		{
			name:   "recap missing",
			output: `{"_event":"v2_playbook_on_start"}`,
			want:   "does not contain v2_playbook_on_stats",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			steps := &ActiveChecks{dryRunLogs: tt.output}
			err := steps.ansibleReportsNoJailChanges(t.Context())
			if err == nil {
				err = steps.ansibleReportsNoFailedOrUnreachableHosts(t.Context())
			}
			if tt.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.want)
			}
		})
	}
}
