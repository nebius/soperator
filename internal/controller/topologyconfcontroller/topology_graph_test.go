package topologyconfcontroller_test

import (
	"context"
	"fmt"
	"strings"
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
			// Tiers are numbered from the root down: tier-1 sits nearest the fabric root and the
			// highest tier a node carries is the switch holding it.
			name: "Two tiers - tier-1 nearest the root, tier-2 holds the nodes",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "pod1", "tier-2": "su1"},
				"node2": {"tier-1": "pod2", "tier-2": "su2"},
				"node3": {"tier-1": "pod1", "tier-2": "su3"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0", "worker-1"},
				"node2": {"worker-2"},
				"node3": {"worker-3"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2", "worker-3", "worker-4", "worker-5"},
			expected: []string{
				"SwitchName=root Switches=pod1,pod2,unknown",
				"SwitchName=pod1 Switches=su1,su3",
				"SwitchName=pod2 Switches=su2",
				"SwitchName=su1 Nodes=worker-[0-1]",
				"SwitchName=su2 Nodes=worker-2",
				"SwitchName=su3 Nodes=worker-3",
				"SwitchName=unknown Nodes=worker-[4-5]",
			},
		},
		{
			// tier-0 is the widest domain, so it hangs off the fabric root and every lower tier
			// descends from it.
			name: "tier-0 is the switch closest to the fabric root",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-0": "spine0", "tier-1": "pod1", "tier-2": "su1"},
				"node2": {"tier-0": "spine0", "tier-1": "pod1", "tier-2": "su2"},
				"node3": {"tier-0": "spine0", "tier-1": "pod2", "tier-2": "su3"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0", "worker-1"},
				"node2": {"worker-2"},
				"node3": {"worker-3"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2", "worker-3", "worker-4"},
			expected: []string{
				"SwitchName=root Switches=spine0,unknown",
				"SwitchName=spine0 Switches=pod1,pod2",
				"SwitchName=pod1 Switches=su1,su2",
				"SwitchName=pod2 Switches=su3",
				"SwitchName=su1 Nodes=worker-[0-1]",
				"SwitchName=su2 Nodes=worker-2",
				"SwitchName=su3 Nodes=worker-3",
				"SwitchName=unknown Nodes=worker-4",
			},
		},
		{
			name: "Distinct tier-0 values form separate subtrees under the root",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-0": "spine0", "tier-1": "leaf1"},
				"node2": {"tier-0": "spine1", "tier-1": "leaf2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0", "worker-1"},
				"node2": {"worker-2"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2"},
			expected: []string{
				"SwitchName=root Switches=spine0,spine1",
				"SwitchName=spine0 Switches=leaf1",
				"SwitchName=spine1 Switches=leaf2",
				"SwitchName=leaf1 Nodes=worker-[0-1]",
				"SwitchName=leaf2 Nodes=worker-2",
			},
		},
		{
			// With no lower tier to descend to, the nodes hang off tier-0 itself, the same shape a
			// tier-1-only node produces.
			name: "tier-0 alone puts the nodes right under the fabric root",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-0": "spine0"},
				"node2": {"tier-0": "spine1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
			},
			allNodeNames: []string{"worker-0", "worker-1"},
			expected: []string{
				"SwitchName=root Switches=spine0,spine1",
				"SwitchName=spine0 Nodes=worker-0",
				"SwitchName=spine1 Nodes=worker-1",
			},
		},
		{
			name: "tier-0 alone honours the NodeSet fabric",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-0": "spine0"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"a-0"},
			},
			allNodeNames: []string{"a-0"},
			fabricByNode: map[string]string{"a-0": "fab-a"},
			expected: []string{
				"SwitchName=fab-a Switches=spine0",
				"SwitchName=spine0 Nodes=a-0",
			},
		},
		{
			// An empty tier-0 is the same as no tier-0: the chain starts at tier-1, which then
			// becomes the switch closest to the root.
			name: "Empty tier-0 value leaves the tier-1 chain untouched",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-0": "", "tier-1": "pod1", "tier-2": "su1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
			},
			allNodeNames: []string{"worker-0"},
			expected: []string{
				"SwitchName=root Switches=pod1",
				"SwitchName=pod1 Switches=su1",
				"SwitchName=su1 Nodes=worker-0",
			},
		},
		{
			// tier-0 does not rescue a broken tier-1..N chain: the node falls back to "unknown"
			// rather than hanging off a path with a level missing from the middle.
			name: "tier-0 with a gap in the tier chain falls back to unknown",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-0": "spine0", "tier-2": "su1"},
				"node2": {"tier-0": "spine1", "tier-1": "", "tier-2": "su2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
			},
			allNodeNames: []string{"worker-0", "worker-1"},
			expected: []string{
				"SwitchName=root Switches=unknown",
				"SwitchName=unknown Nodes=worker-[0-1]",
			},
		},
		{
			// A fabric with fewer levels than the label set repeats one switch across two tiers.
			// The repeat collapses: a switch that is its own parent is unreachable from the root.
			name: "tier-0 repeating its tier-1 value collapses into one switch",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-0": "spine0", "tier-1": "spine0", "tier-2": "su1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
			},
			allNodeNames: []string{"worker-0"},
			expected: []string{
				"SwitchName=root Switches=spine0",
				"SwitchName=spine0 Switches=su1",
				"SwitchName=su1 Nodes=worker-0",
			},
		},
		{
			// A name repeated at two non-adjacent tiers would close a cycle (sw-a -> sw-b -> sw-a)
			// that no fabric root reaches, taking the whole subtree out of the tree with it. The
			// deepest occurrence wins, since that is the switch the node hangs off.
			name: "Tier value repeated at a distant tier does not close a cycle",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "sw-a", "tier-2": "sw-b", "tier-3": "sw-a"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
			},
			allNodeNames: []string{"worker-0"},
			expected: []string{
				"SwitchName=root Switches=sw-b",
				"SwitchName=sw-b Switches=sw-a",
				"SwitchName=sw-a Nodes=worker-0",
			},
		},
		{
			// Regression (SCHED-1971) at the tier-0 level: a name whose trailing decimal run
			// exceeds the uint64 range must be terminated identically where it is declared and
			// where its parent references it.
			name: "tier-0 name with an overflowing decimal tail is sanitized consistently",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {
					"tier-0": "spine12345678901234567890",
					"tier-1": "leaf1",
				},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
			},
			allNodeNames: []string{"worker-0"},
			expected: []string{
				"SwitchName=root Switches=spine12345678901234567890_",
				"SwitchName=spine12345678901234567890_ Switches=leaf1",
				"SwitchName=leaf1 Nodes=worker-0",
			},
		},
		{
			name: "Without unknown - all nodes placed on switches",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "pod1", "tier-2": "su1"},
				"node2": {"tier-1": "pod2", "tier-2": "su2"},
				"node3": {"tier-1": "pod1", "tier-2": "su3"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0", "worker-1"},
				"node2": {"worker-2"},
				"node3": {"worker-3"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2", "worker-3"},
			expected: []string{
				"SwitchName=root Switches=pod1,pod2",
				"SwitchName=pod1 Switches=su1,su3",
				"SwitchName=pod2 Switches=su2",
				"SwitchName=su1 Nodes=worker-[0-1]",
				"SwitchName=su2 Nodes=worker-2",
				"SwitchName=su3 Nodes=worker-3",
			},
		},
		{
			name: "Complex 3-tier topology",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1", "tier-3": "switch0"},
				"node2": {"tier-1": "spine1", "tier-2": "leaf1", "tier-3": "switch1"},
				"node3": {"tier-1": "spine1", "tier-2": "leaf2", "tier-3": "switch2"},
				"node4": {"tier-1": "spine3", "tier-2": "leaf3", "tier-3": "switch3"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
				"node3": {"worker-2"},
				"node4": {"worker-3"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2", "worker-3"},
			expected: []string{
				"SwitchName=root Switches=spine1,spine3",
				"SwitchName=spine1 Switches=leaf1,leaf2",
				"SwitchName=spine3 Switches=leaf3",
				"SwitchName=leaf1 Switches=switch0,switch1",
				"SwitchName=leaf2 Switches=switch2",
				"SwitchName=leaf3 Switches=switch3",
				"SwitchName=switch0 Nodes=worker-0",
				"SwitchName=switch1 Nodes=worker-1",
				"SwitchName=switch2 Nodes=worker-2",
				"SwitchName=switch3 Nodes=worker-3",
			},
		},
		{
			name: "Single tier topology",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "leaf1"},
				"node2": {"tier-1": "leaf2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
			},
			allNodeNames: []string{"worker-0", "worker-1"},
			expected: []string{
				"SwitchName=root Switches=leaf1,leaf2",
				"SwitchName=leaf1 Nodes=worker-0",
				"SwitchName=leaf2 Nodes=worker-1",
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
				"node1": {"tier-1": "pod1", "tier-2": "su1"},
			},
			gpuPodsByNode: map[string][]string{},
			allNodeNames:  []string{"gpu-0", "gpu-1", "cpu-0"},
			expected: []string{
				"SwitchName=root Switches=unknown",
				"SwitchName=unknown Nodes=cpu-0,gpu-[0-1]",
			},
		},
		{
			name: "Two tier topology",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1"},
				"node2": {"tier-1": "spine1", "tier-2": "leaf2"},
				"node3": {"tier-1": "spine2", "tier-2": "leaf3"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
				"node3": {"worker-2"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2"},
			expected: []string{
				"SwitchName=root Switches=spine1,spine2",
				"SwitchName=spine1 Switches=leaf1,leaf2",
				"SwitchName=spine2 Switches=leaf3",
				"SwitchName=leaf1 Nodes=worker-0",
				"SwitchName=leaf2 Nodes=worker-1",
				"SwitchName=leaf3 Nodes=worker-2",
			},
		},
		{
			name: "Four tier topology",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "core1", "tier-2": "spine1", "tier-3": "leaf1", "tier-4": "switch1"},
				"node2": {"tier-1": "core1", "tier-2": "spine1", "tier-3": "leaf1", "tier-4": "switch2"},
				"node3": {"tier-1": "core2", "tier-2": "spine2", "tier-3": "leaf2", "tier-4": "switch3"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
				"node3": {"worker-2"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2"},
			expected: []string{
				"SwitchName=root Switches=core1,core2",
				"SwitchName=core1 Switches=spine1",
				"SwitchName=core2 Switches=spine2",
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=spine2 Switches=leaf2",
				"SwitchName=leaf1 Switches=switch1,switch2",
				"SwitchName=leaf2 Switches=switch3",
				"SwitchName=switch1 Nodes=worker-0",
				"SwitchName=switch2 Nodes=worker-1",
				"SwitchName=switch3 Nodes=worker-2",
			},
		},
		{
			// Heterogeneous depth: a switch that holds nodes directly and also has child switches
			// gets a synthetic leaf, because Slurm ignores children once a switch lists nodes.
			name: "Incomplete tier topology",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1", "tier-3": "switch1"},
				"node2": {"tier-1": "spine1", "tier-2": "leaf1"},
				"node3": {"tier-1": "spine1"},
				"node4": {"tier-2": "leaf2", "tier-3": "switch2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
				"node3": {"worker-2"},
				"node4": {"worker-3"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2", "worker-3"},
			expected: []string{
				"SwitchName=root Switches=spine1,unknown",
				"SwitchName=spine1 Switches=leaf1,spine1.nodes",
				"SwitchName=spine1.nodes Nodes=worker-2",
				"SwitchName=leaf1 Switches=leaf1.nodes,switch1",
				"SwitchName=leaf1.nodes Nodes=worker-1",
				"SwitchName=switch1 Nodes=worker-0",
				"SwitchName=unknown Nodes=worker-3",
			},
		},
		{
			name: "Duplicate devices in same tier",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1"},
				"node2": {"tier-1": "spine1", "tier-2": "leaf1"},
				"node3": {"tier-1": "spine1", "tier-2": "leaf2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
				"node3": {"worker-2"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2"},
			expected: []string{
				"SwitchName=root Switches=spine1",
				"SwitchName=spine1 Switches=leaf1,leaf2",
				"SwitchName=leaf1 Nodes=worker-[0-1]",
				"SwitchName=leaf2 Nodes=worker-2",
			},
		},
		{
			// Inconsistent labelling: two nodes name the same tier-2 switch under different
			// tier-1 parents. The switch then has two parents, which is a graph rather than the
			// tree Slurm expects -- pinned so the shape is visible if such labels appear.
			name: "Same tier-2 switch under two tier-1 parents",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1"},
				"node2": {"tier-1": "spine2", "tier-2": "leaf1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
			},
			allNodeNames: []string{"worker-0", "worker-1"},
			expected: []string{
				"SwitchName=root Switches=spine1,spine2",
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=spine2 Switches=leaf1",
				"SwitchName=leaf1 Nodes=worker-[0-1]",
			},
		},
		{
			name: "Complex topology with many connections",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1"},
				"node2": {"tier-1": "spine2", "tier-2": "leaf2"},
				"node3": {"tier-1": "spine3", "tier-2": "leaf3"},
				"node4": {"tier-1": "spine1", "tier-2": "leaf4"},
				"node5": {"tier-1": "spine2", "tier-2": "leaf5"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
				"node3": {"worker-2"},
				"node4": {"worker-3"},
				"node5": {"worker-4"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2", "worker-3", "worker-4"},
			expected: []string{
				"SwitchName=root Switches=spine1,spine2,spine3",
				"SwitchName=spine1 Switches=leaf1,leaf4",
				"SwitchName=spine2 Switches=leaf2,leaf5",
				"SwitchName=spine3 Switches=leaf3",
				"SwitchName=leaf1 Nodes=worker-0",
				"SwitchName=leaf2 Nodes=worker-1",
				"SwitchName=leaf3 Nodes=worker-2",
				"SwitchName=leaf4 Nodes=worker-3",
				"SwitchName=leaf5 Nodes=worker-4",
			},
		},
		{
			name: "Empty tier values",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "", "tier-2": "leaf1"},
				"node2": {"tier-1": "spine1", "tier-2": ""},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
			},
			allNodeNames: []string{"worker-0", "worker-1"},
			expected: []string{
				"SwitchName=root Switches=unknown",
				"SwitchName=unknown Nodes=worker-[0-1]",
			},
		},
		{
			name: "Single node per tier level",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1", "tier-3": "switch1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
			},
			allNodeNames: []string{"worker-0"},
			expected: []string{
				"SwitchName=root Switches=spine1",
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=leaf1 Switches=switch1",
				"SwitchName=switch1 Nodes=worker-0",
			},
		},
		{
			name: "Check result sorting",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "z-spine", "tier-2": "z-leaf"},
				"node2": {"tier-1": "a-spine", "tier-2": "a-leaf"},
				"node3": {"tier-1": "m-spine", "tier-2": "m-leaf"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
				"node3": {"worker-2"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2"},
			expected: []string{
				"SwitchName=root Switches=a-spine,m-spine,z-spine",
				"SwitchName=a-spine Switches=a-leaf",
				"SwitchName=a-leaf Nodes=worker-1",
				"SwitchName=m-spine Switches=m-leaf",
				"SwitchName=m-leaf Nodes=worker-2",
				"SwitchName=z-spine Switches=z-leaf",
				"SwitchName=z-leaf Nodes=worker-0",
			},
		},
		{
			name: "Multiple pods per node",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1"},
				"node2": {"tier-1": "spine1", "tier-2": "leaf2"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0", "worker-1", "worker-2"},
				"node2": {"worker-3", "worker-4"},
			},
			allNodeNames: []string{"worker-0", "worker-1", "worker-2", "worker-3", "worker-4", "worker-5"},
			expected: []string{
				"SwitchName=root Switches=spine1,unknown",
				"SwitchName=spine1 Switches=leaf1,leaf2",
				"SwitchName=leaf1 Nodes=worker-[0-2]",
				"SwitchName=leaf2 Nodes=worker-[3-4]",
				"SwitchName=unknown Nodes=worker-5",
			},
		},
		{
			name: "Nodes with missing pod assignments should not create invalid switches",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine-X", "tier-2": "leaf-A"},
				"node2": {"tier-1": "spine-X", "tier-2": "leaf-B"},
				"node3": {"tier-1": "spine-X", "tier-2": "leaf-C"}, // This node has no pods!
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
				"SwitchName=spine-X Switches=leaf-A,leaf-B", // Should NOT include leaf-C
				"SwitchName=leaf-A Nodes=worker-1",
				"SwitchName=leaf-B Nodes=worker-2",
			},
		},
		{
			name: "Two NodeSet fabrics produce two unconnected fabric roots",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1"},
				"node2": {"tier-1": "spine2", "tier-2": "leaf2"},
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
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=spine2 Switches=leaf2",
				"SwitchName=leaf1 Nodes=a-0",
				"SwitchName=leaf2 Nodes=b-0",
			},
		},
		{
			name: "Fabric is the root of the tier-1/tier-2 path",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"a-0"},
			},
			allNodeNames: []string{"a-0"},
			fabricByNode: map[string]string{"a-0": "cluster-x"},
			expected: []string{
				"SwitchName=cluster-x Switches=spine1",
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=leaf1 Nodes=a-0",
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
				"SwitchName=fab-a.unknown Nodes=a-[0-1]",
				"SwitchName=fab-b.unknown Nodes=b-0",
			},
		},
		{
			name: "Mixed: explicit fabric NodeSet and defaulted NodeSet",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1"},
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
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=leaf1 Nodes=a-0",
				"SwitchName=root Switches=unknown",
				"SwitchName=unknown Nodes=def-0",
			},
		},
		{
			// Regression: a switch ID whose trailing decimal run exceeds the uint64 range
			// (here a 20-digit tail) must be terminated identically wherever it appears, so the
			// parent's Switches= reference still matches the child's SwitchName= line and Slurm
			// does not overflow it to UINT64_MAX (SCHED-1971).
			name: "Switch ID with overflowing decimal tail is sanitized consistently",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {
					"tier-1": "66b2be03e8b30ab5bcf8c9fd57d6c293",
					"tier-2": "6f84b74219aa22869602735141708147",
				},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"worker-0"},
			},
			allNodeNames: []string{"worker-0"},
			expected: []string{
				"SwitchName=root Switches=66b2be03e8b30ab5bcf8c9fd57d6c293",
				"SwitchName=66b2be03e8b30ab5bcf8c9fd57d6c293 Switches=6f84b74219aa22869602735141708147_",
				"SwitchName=6f84b74219aa22869602735141708147_ Nodes=worker-0",
			},
		},
		{
			name: "Running node with tiers but no fabric stays under root",
			labelsByNode: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "spine1", "tier-2": "leaf1"},
			},
			gpuPodsByNode: map[string][]string{
				"node1": {"a-0"},
			},
			allNodeNames: []string{"a-0"},
			expected: []string{
				"SwitchName=root Switches=spine1",
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=leaf1 Nodes=a-0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph := tc.BuildTopologyGraph(context.Background(), tt.labelsByNode, tt.gpuPodsByNode, tt.allNodeNames, tt.fabricByNode)
			result := renderedSwitchLines(graph)
			require.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestRenderTopologyConfig_MergesSwitches(t *testing.T) {
	labelsByNode := map[string]tc.NodeTopologyLabels{
		"node1": {"tier-1": "spine-0", "tier-2": "leaf-0"},
		"node2": {"tier-1": "spine-0", "tier-2": "leaf-1"},
		"node3": {"tier-1": "spine-0", "tier-2": "leaf-cpu-0"},
		"node4": {"tier-1": "spine-0", "tier-2": "leaf-cpu-2"},
		"node5": {"tier-1": "spine-0", "tier-2": "leafkek1"},
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
	lines := renderedSwitchLines(graph)

	require.Contains(t, lines, "SwitchName=spine-0 Switches=leaf-[0-1],leaf-cpu-[0,2],leafkek1")
}

func TestRenderTopologyConfig_DirectNodeLeafNames(t *testing.T) {
	for _, parent := range []string{"leaf1", strings.Repeat("a", 64)} {
		t.Run(parent, func(t *testing.T) {
			base := parent[:min(len(parent), 58)]
			graph := tc.BuildTopologyGraph(context.Background(), map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": parent, "tier-2": base + ".nodes"},
				"node2": {"tier-1": parent},
			}, map[string][]string{
				"node1": {"worker-0"},
				"node2": {"worker-1"},
			}, []string{"worker-0", "worker-1"}, nil)

			synthetic := parent[:min(len(parent), 57)] + ".nodes1"
			require.Contains(t, renderedSwitchLines(graph), "SwitchName="+synthetic+" Nodes=worker-1")
			var nodeLists []string
			for _, sw := range graph.RenderSwitches() {
				require.LessOrEqual(t, len(sw.Switch), 64)
				if sw.Nodes != "" {
					nodeLists = append(nodeLists, sw.Nodes)
				}
			}
			require.ElementsMatch(t, []string{"worker-0", "worker-1"}, nodeLists)
		})
	}
}

// renderedSwitchLines formats the switch entries as single lines, keeping these
// assertions readable now that the only rendered format is topology.yaml.
func renderedSwitchLines(graph tc.TopologyGraph) []string {
	var lines []string
	for _, sw := range graph.RenderSwitches() {
		if sw.Children != "" {
			lines = append(lines, fmt.Sprintf("SwitchName=%s Switches=%s", sw.Switch, sw.Children))
			continue
		}
		lines = append(lines, fmt.Sprintf("SwitchName=%s Nodes=%s", sw.Switch, sw.Nodes))
	}
	return lines
}
