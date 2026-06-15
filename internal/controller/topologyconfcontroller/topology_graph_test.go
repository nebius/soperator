package topologyconfcontroller_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	tc "nebius.ai/slurm-operator/internal/controller/topologyconfcontroller"
)

func TestRenderTopologyConfig(t *testing.T) {
	tests := []struct {
		name          string
		labelsByNode  map[string]tc.NodeTopologyLabels
		gpuPodsByNode map[string][]string
		allNodeNames  []string
		fabricByNode  map[string]string
		expected      []string
	}{
		{
			name: "With root node - combined tier1 and higher tiers",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
				"node2": {"tier-1": "switch2", "tier-2": "spine2"},
				"node3": {"tier-1": "switch1", "tier-2": "spine3"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"pod1", "pod2"},
				"node2": {"pod3"},
				"node3": {"pod4"},
			},
			allNodeNames: []string{"pod1", "pod2", "pod3", "pod4", "pod5", "pod6"},
			expected: []string{
				"SwitchName=root Switches=spine1,spine2,spine3,unknown",
				"SwitchName=spine1 Switches=switch1",
				"SwitchName=spine2 Switches=switch2",
				"SwitchName=spine3 Switches=switch1",
				"SwitchName=switch1 Nodes=pod1,pod2,pod4",
				"SwitchName=switch2 Nodes=pod3",
				"SwitchName=unknown Nodes=pod5,pod6",
			},
		},
		{
			name: "With root node and tier-0 label - combined tier1 and higher tiers",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-0": "block0", "tier-1": "switch1", "tier-2": "spine1"},
				"node2": {"tier-0": "block0", "tier-1": "switch2", "tier-2": "spine2"},
				"node3": {"tier-0": "block0", "tier-1": "switch1", "tier-2": "spine3"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"pod1", "pod2"},
				"node2": {"pod3"},
				"node3": {"pod4"},
			},
			allNodeNames: []string{"pod1", "pod2", "pod3", "pod4", "pod5", "pod6"},
			expected: []string{
				"SwitchName=root Switches=spine1,spine2,spine3,unknown",
				"SwitchName=spine1 Switches=switch1",
				"SwitchName=spine2 Switches=switch2",
				"SwitchName=spine3 Switches=switch1",
				"SwitchName=switch1 Nodes=pod1,pod2,pod4",
				"SwitchName=switch2 Nodes=pod3",
				"SwitchName=unknown Nodes=pod5,pod6",
			},
		},
		{
			name: "Without unknown - all nodes placed on switches",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
				"node2": {"tier-1": "switch2", "tier-2": "spine2"},
				"node3": {"tier-1": "switch1", "tier-2": "spine3"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"pod1", "pod2"},
				"node2": {"pod3"},
				"node3": {"pod4"},
			},
			allNodeNames: []string{"pod1", "pod2", "pod3", "pod4"},
			expected: []string{
				"SwitchName=root Switches=spine1,spine2,spine3",
				"SwitchName=spine1 Switches=switch1",
				"SwitchName=spine2 Switches=switch2",
				"SwitchName=spine3 Switches=switch1",
				"SwitchName=switch1 Nodes=pod1,pod2,pod4",
				"SwitchName=switch2 Nodes=pod3",
			},
		},
		{
			name: "Complex 3-tier topology",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch0", "tier-2": "leaf1", "tier-3": "spine1"},
				"node2": {"tier-1": "switch1", "tier-2": "leaf1", "tier-3": "spine1"},
				"node3": {"tier-1": "switch2", "tier-2": "leaf2", "tier-3": "spine1"},
				"node4": {"tier-1": "switch3", "tier-2": "leaf3", "tier-3": "spine3"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"node1"},
				"node2": {"node2"},
				"node3": {"node3"},
				"node4": {"node4"},
			},
			allNodeNames: []string{"node1", "node2", "node3", "node4"},
			expected: []string{
				"SwitchName=root Switches=spine1,spine3",
				"SwitchName=leaf1 Switches=switch0,switch1",
				"SwitchName=leaf2 Switches=switch2",
				"SwitchName=leaf3 Switches=switch3",
				"SwitchName=spine1 Switches=leaf1,leaf2",
				"SwitchName=spine3 Switches=leaf3",
				"SwitchName=switch0 Nodes=node1",
				"SwitchName=switch1 Nodes=node2",
				"SwitchName=switch2 Nodes=node3",
				"SwitchName=switch3 Nodes=node4",
			},
		},
		{
			name: "Single tier topology",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1"},
				"node2": {"tier-1": "switch2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"node1"},
				"node2": {"node2"},
			},
			allNodeNames: []string{"node1", "node2"},
			expected: []string{
				"SwitchName=root Switches=switch1,switch2",
				"SwitchName=switch1 Nodes=node1",
				"SwitchName=switch2 Nodes=node2",
			},
		},
		{
			name:          "Empty topology",
			labelsByNode:  map[string]tc.NodeTopologyLabels{},
			gpuPodsByNode: map[string][]string{},
			allNodeNames:  nil,
			expected:      []string{},
		},
		{
			name: "All nodes powered down - present under unknown",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
			},
			gpuPodsByNode: map[string][]string{},
			allNodeNames:  []string{"gpu-0", "gpu-1", "cpu-0"},
			expected: []string{
				"SwitchName=root Switches=unknown",
				"SwitchName=unknown Nodes=cpu-0,gpu-0,gpu-1",
			},
		},
		{
			name: "Two tier topology",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
				"node2": {"tier-1": "switch2", "tier-2": "spine1"},
				"node3": {"tier-1": "switch3", "tier-2": "spine2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"node1"},
				"node2": {"node2"},
				"node3": {"node3"},
			},
			allNodeNames: []string{"node1", "node2", "node3"},
			expected: []string{
				"SwitchName=root Switches=spine1,spine2",
				"SwitchName=spine1 Switches=switch1,switch2",
				"SwitchName=spine2 Switches=switch3",
				"SwitchName=switch1 Nodes=node1",
				"SwitchName=switch2 Nodes=node2",
				"SwitchName=switch3 Nodes=node3",
			},
		},
		{
			name: "Four tier topology",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1", "tier-3": "spine1", "tier-4": "core1"},
				"node2": {"tier-1": "switch2", "tier-2": "leaf1", "tier-3": "spine1", "tier-4": "core1"},
				"node3": {"tier-1": "switch3", "tier-2": "leaf2", "tier-3": "spine2", "tier-4": "core2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"node1"},
				"node2": {"node2"},
				"node3": {"node3"},
			},
			allNodeNames: []string{"node1", "node2", "node3"},
			expected: []string{
				"SwitchName=root Switches=core1,core2",
				"SwitchName=core1 Switches=spine1",
				"SwitchName=core2 Switches=spine2",
				"SwitchName=leaf1 Switches=switch1,switch2",
				"SwitchName=leaf2 Switches=switch3",
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=spine2 Switches=leaf2",
				"SwitchName=switch1 Nodes=node1",
				"SwitchName=switch2 Nodes=node2",
				"SwitchName=switch3 Nodes=node3",
			},
		},
		{
			name: "Incomplete tier topology",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1", "tier-3": "spine1"},
				"node2": {"tier-1": "switch2", "tier-2": "leaf1"},
				"node3": {"tier-1": "switch3"},
				"node4": {"tier-2": "leaf2", "tier-3": "spine2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"node1"},
				"node2": {"node2"},
				"node3": {"node3"},
				"node4": {"node4"},
			},
			allNodeNames: []string{"node1", "node2", "node3", "node4"},
			expected: []string{
				"SwitchName=root Switches=spine1,switch3,unknown",
				"SwitchName=leaf1 Switches=switch1,switch2",
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=switch1 Nodes=node1",
				"SwitchName=switch2 Nodes=node2",
				"SwitchName=switch3 Nodes=node3",
				"SwitchName=unknown Nodes=node4",
			},
		},
		{
			name: "Duplicate devices in same tier",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1"},
				"node2": {"tier-1": "switch1", "tier-2": "leaf1"},
				"node3": {"tier-1": "switch2", "tier-2": "leaf1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"node1"},
				"node2": {"node2"},
				"node3": {"node3"},
			},
			allNodeNames: []string{"node1", "node2", "node3"},
			expected: []string{
				"SwitchName=root Switches=leaf1",
				"SwitchName=leaf1 Switches=switch1,switch2",
				"SwitchName=switch1 Nodes=node1,node2",
				"SwitchName=switch2 Nodes=node3",
			},
		},
		{
			name: "Complex topology with many connections",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1"},
				"node2": {"tier-1": "switch2", "tier-2": "leaf2"},
				"node3": {"tier-1": "switch3", "tier-2": "leaf3"},
				"node4": {"tier-1": "switch4", "tier-2": "leaf1"},
				"node5": {"tier-1": "switch5", "tier-2": "leaf2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"node1"},
				"node2": {"node2"},
				"node3": {"node3"},
				"node4": {"node4"},
				"node5": {"node5"},
			},
			allNodeNames: []string{"node1", "node2", "node3", "node4", "node5"},
			expected: []string{
				"SwitchName=root Switches=leaf1,leaf2,leaf3",
				"SwitchName=leaf1 Switches=switch1,switch4",
				"SwitchName=leaf2 Switches=switch2,switch5",
				"SwitchName=leaf3 Switches=switch3",
				"SwitchName=switch1 Nodes=node1",
				"SwitchName=switch2 Nodes=node2",
				"SwitchName=switch3 Nodes=node3",
				"SwitchName=switch4 Nodes=node4",
				"SwitchName=switch5 Nodes=node5",
			},
		},
		{
			name: "Empty tier values",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "", "tier-2": "leaf1"},
				"node2": {"tier-1": "switch1", "tier-2": ""},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"node1"},
				"node2": {"node2"},
			},
			allNodeNames: []string{"node1", "node2"},
			expected: []string{
				"SwitchName=root Switches=unknown",
				"SwitchName=unknown Nodes=node1,node2",
			},
		},
		{
			name: "Single node per tier level",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1", "tier-3": "spine1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"node1"},
			},
			allNodeNames: []string{"node1"},
			expected: []string{
				"SwitchName=root Switches=spine1",
				"SwitchName=leaf1 Switches=switch1",
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=switch1 Nodes=node1",
			},
		},
		{
			name: "Check result sorting",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "z-switch", "tier-2": "z-leaf"},
				"node2": {"tier-1": "a-switch", "tier-2": "a-leaf"},
				"node3": {"tier-1": "m-switch", "tier-2": "m-leaf"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"node1"},
				"node2": {"node2"},
				"node3": {"node3"},
			},
			allNodeNames: []string{"node1", "node2", "node3"},
			expected: []string{
				"SwitchName=root Switches=a-leaf,m-leaf,z-leaf",
				"SwitchName=a-leaf Switches=a-switch",
				"SwitchName=a-switch Nodes=node2",
				"SwitchName=m-leaf Switches=m-switch",
				"SwitchName=m-switch Nodes=node3",
				"SwitchName=z-leaf Switches=z-switch",
				"SwitchName=z-switch Nodes=node1",
			},
		},
		{
			name: "Multiple pods per node",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
				"node2": {"tier-1": "switch2", "tier-2": "spine1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"pod1", "pod2", "pod3"},
				"node2": {"pod4", "pod5"},
			},
			allNodeNames: []string{"pod1", "pod2", "pod3", "pod4", "pod5", "pod6"},
			expected: []string{
				"SwitchName=root Switches=spine1,unknown",
				"SwitchName=spine1 Switches=switch1,switch2",
				"SwitchName=switch1 Nodes=pod1,pod2,pod3",
				"SwitchName=switch2 Nodes=pod4,pod5",
				"SwitchName=unknown Nodes=pod6",
			},
		},
		{
			name: "Nodes with missing pod assignments should not create invalid switches",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "leaf-A", "tier-2": "spine-X"},
				"node2": {"tier-1": "leaf-B", "tier-2": "spine-X"},
				"node3": {"tier-1": "leaf-C", "tier-2": "spine-X"}, // This node has no pods!
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-1"},
				"node2": {"worker-2"},
				// "node3" is missing - this should not create invalid topology
			},
			allNodeNames: []string{"worker-1", "worker-2"},
			expected: []string{
				// This is what SHOULD be generated (without leaf-C):
				"SwitchName=root Switches=spine-X",
				"SwitchName=leaf-A Nodes=worker-1",
				"SwitchName=leaf-B Nodes=worker-2",
				"SwitchName=spine-X Switches=leaf-A,leaf-B", // Should NOT include leaf-C
			},
		},
		{
			name: "Two NodeSet fabrics produce two unconnected fabric roots",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
				"node2": {"tier-1": "switch2", "tier-2": "spine2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"a-0"},
				"node2": {"b-0"},
			},
			allNodeNames: []string{"a-0", "b-0"},
			fabricByNode: map[string]string{
				"a-0": "fab-a",
				"b-0": "fab-b",
			},
			expected: []string{
				"SwitchName=fab-a Switches=spine1",
				"SwitchName=fab-b Switches=spine2",
				"SwitchName=spine1 Switches=switch1",
				"SwitchName=spine2 Switches=switch2",
				"SwitchName=switch1 Nodes=a-0",
				"SwitchName=switch2 Nodes=b-0",
			},
		},
		{
			name: "Fabric is the root of the tier-1/tier-2 path",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"a-0"},
			},
			allNodeNames: []string{"a-0"},
			fabricByNode: map[string]string{"a-0": "cluster-x"},
			expected: []string{
				"SwitchName=cluster-x Switches=spine1",
				"SwitchName=spine1 Switches=switch1",
				"SwitchName=switch1 Nodes=a-0",
			},
		},
		{
			name:          "Powered-down nodes land under their fabric's unknown switch",
			labelsByNode:  map[string]tc.NodeTopologyLabels{},
			gpuPodsByNode: map[string][]string{},
			allNodeNames:  []string{"a-0", "a-1", "b-0"},
			fabricByNode: map[string]string{
				"a-0": "fab-a",
				"a-1": "fab-a",
				"b-0": "fab-b",
			},
			expected: []string{
				"SwitchName=fab-a Switches=fab-a.unknown",
				"SwitchName=fab-b Switches=fab-b.unknown",
				"SwitchName=fab-a.unknown Nodes=a-0,a-1",
				"SwitchName=fab-b.unknown Nodes=b-0",
			},
		},
		{
			name: "Mixed: explicit fabric NodeSet and defaulted NodeSet",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"a-0"},
			},
			allNodeNames: []string{"a-0", "def-0"},
			fabricByNode: map[string]string{
				"a-0": "fab-a",
				// "def-0" has no fabric -> defaults to "root"/"unknown".
			},
			expected: []string{
				"SwitchName=fab-a Switches=spine1",
				"SwitchName=spine1 Switches=switch1",
				"SwitchName=switch1 Nodes=a-0",
				"SwitchName=root Switches=unknown",
				"SwitchName=unknown Nodes=def-0",
			},
		},
		{
			name: "Running node with tiers but no fabric stays under root",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"a-0"},
			},
			allNodeNames: []string{"a-0"},
			expected: []string{
				"SwitchName=root Switches=spine1",
				"SwitchName=spine1 Switches=switch1",
				"SwitchName=switch1 Nodes=a-0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph := tc.BuildTopologyGraph(context.Background(), tt.labelsByNode, tt.gpuPodsByNode, tt.allNodeNames, tt.fabricByNode)
			result := graph.RenderConfigLines()
			require.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestRenderTopologyConfig_MergesSwitches(t *testing.T) {
	labelsByNode := map[string]tc.NodeTopologyLabels{
		"node1": {"tier-1": "leaf-0", "tier-2": "spine-0"},
		"node2": {"tier-1": "leaf-1", "tier-2": "spine-0"},
		"node3": {"tier-1": "leaf-cpu-0", "tier-2": "spine-0"},
		"node4": {"tier-1": "leaf-cpu-2", "tier-2": "spine-0"},
		"node5": {"tier-1": "leafkek1", "tier-2": "spine-0"},
	}
	podsByNode := map[string][]string{
		"node1": {"worker-a"},
		"node2": {"worker-b"},
		"node3": {"worker-c"},
		"node4": {"worker-d"},
		"node5": {"worker-e"},
	}

	allNodeNames := []string{"worker-a", "worker-b", "worker-c", "worker-d", "worker-e"}

	graph := tc.BuildTopologyGraph(context.Background(), labelsByNode, podsByNode, allNodeNames, nil)
	lines := graph.RenderConfigLines()

	require.Contains(t, lines, "SwitchName=spine-0 Switches=leaf-[0-1],leaf-cpu-[0,2],leafkek1")
}
