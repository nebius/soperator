package topologyconfcontroller

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTierZeroMergesNodesFromDifferentSubtrees pins the point of the tier-0 level: nodes that
// share it converge on one switch near the root, whatever their lower tiers are.
func TestTierZeroMergesNodesFromDifferentSubtrees(t *testing.T) {
	graph := BuildTopologyGraph(context.Background(), map[string]NodeTopologyLabels{
		"node1": {"tier-0": "spine1", "tier-1": "leaf1"},
		"node2": {"tier-0": "spine1", "tier-1": "leaf2"},
	}, map[string][]string{
		"node1": {"worker-0"},
		"node2": {"worker-1"},
	}, []string{"worker-0", "worker-1"}, nil)

	require.Equal(t, []switchYAML{
		{Switch: "leaf1", Nodes: "worker-0"},
		{Switch: "leaf2", Nodes: "worker-1"},
		{Switch: "root", Children: "spine1"},
		{Switch: "spine1", Children: "leaf1,leaf2"},
	}, graph.RenderSwitches())
}

// TestTierZeroRepeatingItsChildTier covers labels that name the same switch twice, which happens
// when a fabric has fewer levels than the label set. The repeat must not make the switch its own
// parent: that would cut the subtree off from the fabric root.
func TestTierZeroRepeatingItsChildTier(t *testing.T) {
	graph := BuildTopologyGraph(context.Background(), map[string]NodeTopologyLabels{
		"node1": {"tier-0": "leaf1", "tier-1": "leaf1"},
	}, map[string][]string{"node1": {"worker-0"}}, []string{"worker-0"}, nil)

	require.Equal(t, []switchYAML{
		{Switch: "leaf1", Nodes: "worker-0"},
		{Switch: "root", Children: "leaf1"},
	}, graph.RenderSwitches())
	for parent, children := range graph.children {
		require.NotContains(t, children, parent, "a switch must not be its own parent")
	}
}

// TestSlurmSafeSwitchName reads the same fixture as worker_init.py's parity test: the terminator
// rule decides which names appear in topology.yaml, so a worker computing it differently would
// register into a switch the config does not declare.
func TestSlurmSafeSwitchName(t *testing.T) {
	data, err := os.ReadFile("testdata/safe_switch_names.json")
	require.NoError(t, err)
	var cases []struct {
		Name string `json:"name"`
		Safe string `json:"safe"`
	}
	require.NoError(t, json.Unmarshal(data, &cases))
	require.NotEmpty(t, cases)

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			require.Equal(t, tc.Safe, slurmSafeSwitchName(tc.Name))
			// 19 digits still fit uint64; the margin below the 20-digit boundary is deliberate.
			require.LessOrEqual(t, trailingDigitCount(tc.Safe), maxSafeTrailingDigits)
		})
	}
}

func TestTrailingDigitCount(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{in: "", want: 0},
		{in: "abc", want: 0},
		{in: "abc1", want: 1},
		{in: "1a2b3", want: 1},
		{in: "12345", want: 5},
		{in: "6f84b74219aa22869602735141708147", want: 20},
	}
	for _, tt := range tests {
		if got := trailingDigitCount(tt.in); got != tt.want {
			t.Errorf("trailingDigitCount(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
