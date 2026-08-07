package topologyconfcontroller_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	tc "nebius.ai/slurm-operator/internal/controller/topologyconfcontroller"
)

func TestBuildTopologyBlocks_GroupsWorkersByTierZero(t *testing.T) {
	labelsByNode := map[string]tc.NodeTopologyLabels{
		"node1": {"tier-0": "block-a"},
		"node2": {"tier-0": "block-a"},
		"node3": {"tier-0": "block-b"},
	}

	{
		gpuPodsByNode := map[string][]string{
			"node1": {"pod1", "pod2"},
			"node2": {"pod3"},
			"node3": {"pod4"},
		}
		allNodeNames := []string{"pod1", "pod2", "pod3", "pod4"}

		blocks := tc.BuildTopologyBlocks(context.Background(), labelsByNode, gpuPodsByNode, allNodeNames, nil)
		lines := blocks.RenderConfigLines()

		require.True(t, len(lines) != 0, "expected non-empty block lines")
		result := parseBlockLines(t, lines)
		require.Equal(t, map[string][]string{
			"block-a": {"pod1", "pod2", "pod3"},
			"block-b": {"pod4"},
		}, result)

		require.Equal(t, map[string][]string{
			"node1": {"pod1", "pod2"},
			"node2": {"pod3"},
			"node3": {"pod4"},
		}, gpuPodsByNode, "BuildTopologyBlocks must not mutate the input gpuPodsByNode map")
	}

	{
		gpuPodsByNode := map[string][]string{
			"node1": {"pod-1", "pod-2"},
			"node2": {"pod-3"},
			"node3": {"pod-4"},
		}
		allNodeNames := []string{"pod-1", "pod-2", "pod-3", "pod-4"}

		blocks := tc.BuildTopologyBlocks(context.Background(), labelsByNode, gpuPodsByNode, allNodeNames, nil)
		lines := blocks.RenderConfigLines()

		require.True(t, len(lines) != 0, "expected non-empty block lines")
		result := parseBlockLines(t, lines)
		require.Equal(t, map[string][]string{
			"block-a": {"pod-[1-3]"},
			"block-b": {"pod-4"},
		}, result)

		require.Equal(t, map[string][]string{
			"node1": {"pod-1", "pod-2"},
			"node2": {"pod-3"},
			"node3": {"pod-4"},
		}, gpuPodsByNode, "BuildTopologyBlocks must not mutate the input gpuPodsByNode map")
	}
}

func TestBuildTopologyBlocks_AssignsUnknownBlock(t *testing.T) {
	labelsByNode := map[string]tc.NodeTopologyLabels{
		"node1": {"tier-0": "block-a"},
		"node2": {}, // missing tier-0 label
	}
	gpuPodsByNode := map[string][]string{
		"node1": {"pod1"},
		"node2": {"pod2"}, // labeled node without tier-0 -> unknown
	}
	// pod3 is a node with no scheduled GPU pod (e.g. powered down or CPU) -> unknown.
	allNodeNames := []string{"pod1", "pod2", "pod3"}

	blocks := tc.BuildTopologyBlocks(context.Background(), labelsByNode, gpuPodsByNode, allNodeNames, nil)
	result := parseBlockLines(t, blocks.RenderConfigLines())

	require.Equal(t, map[string][]string{
		"block-a": {"pod1"},
		"unknown": {"pod2", "pod3"},
	}, result)
}

func TestBuildTopologyBlocks_RenderMergesWorkerNodes(t *testing.T) {
	labelsByNode := map[string]tc.NodeTopologyLabels{
		"node1": {"tier-0": "block-a"},
		"node2": {"tier-0": "block-a"},
	}
	podsByNode := map[string][]string{
		"node1": {"worker-0", "worker-1", "worker-cpu-0"},
		"node2": {"worker-2", "worker-cpu-1", "workerkek1"},
	}

	allNodeNames := []string{"worker-0", "worker-1", "worker-cpu-0", "worker-2", "worker-cpu-1", "workerkek1"}

	blocks := tc.BuildTopologyBlocks(context.Background(), labelsByNode, podsByNode, allNodeNames, nil)
	lines := blocks.RenderConfigLines()

	require.Equal(t, []string{
		"BlockName=block-a Nodes=worker-[0-2],worker-cpu-[0-1],workerkek1",
	}, lines)
}

// Regression (SCHED-1971): a block name (external tier-0 label) ending in an overflowing decimal
// run must be terminated so Slurm's hostlist parser does not rewrite it; the worker list stays
// verbatim.
func TestBuildTopologyBlocks_SanitizesBlockName(t *testing.T) {
	labelsByNode := map[string]tc.NodeTopologyLabels{
		"node1": {"tier-0": "6f84b74219aa22869602735141708147"},
	}
	gpuPodsByNode := map[string][]string{
		"node1": {"worker-0"},
	}
	allNodeNames := []string{"worker-0"}

	blocks := tc.BuildTopologyBlocks(context.Background(), labelsByNode, gpuPodsByNode, allNodeNames, nil)
	lines := blocks.RenderConfigLines()

	require.Equal(t, []string{
		"BlockName=6f84b74219aa22869602735141708147_ Nodes=worker-0",
	}, lines)
}

func TestBuildTopologyBlocks_RenderEmpty(t *testing.T) {
	blocks := tc.BuildTopologyBlocks(context.Background(), map[string]tc.NodeTopologyLabels{}, map[string][]string{}, nil, nil)
	require.Nil(t, blocks.RenderConfigLines())
}

// Powered-down nodes from different fabrics must not share one "unknown" block.
func TestBuildTopologyBlocks_PerFabricUnknown(t *testing.T) {
	allNodeNames := []string{"a-0", "a-1", "b-0", "def-0"}
	fabricByNode := map[string]string{
		"a-0": "fab-a",
		"a-1": "fab-a",
		"b-0": "fab-b",
		// "def-0" has no fabric -> defaults to the legacy "unknown" block.
	}

	blocks := tc.BuildTopologyBlocks(
		context.Background(),
		map[string]tc.NodeTopologyLabels{}, map[string][]string{}, allNodeNames, fabricByNode,
	)

	require.Equal(t, map[string][]string{
		"fab-a.unknown": {"a-[0-1]"},
		"fab-b.unknown": {"b-0"},
		"unknown":       {"def-0"},
	}, parseBlockLines(t, blocks.RenderConfigLines()))
}

func parseBlockLines(t *testing.T, lines []string) map[string][]string {
	t.Helper()

	result := make(map[string][]string, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, " ")
		require.Len(t, parts, 2, "unexpected block line format")

		require.True(t, strings.HasPrefix(parts[0], "BlockName="), "unexpected block name")
		require.True(t, strings.HasPrefix(parts[1], "Nodes="), "unexpected node list")

		blockName := strings.TrimPrefix(parts[0], "BlockName=")
		nodes := strings.Split(strings.TrimPrefix(parts[1], "Nodes="), ",")
		slices.Sort(nodes)
		result[blockName] = nodes
	}
	return result
}
