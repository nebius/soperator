package topology

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_unknownLeafName_Basic(t *testing.T) {
	assert.Equal(t, "unknown", unknownLeafName(DefaultFabric))
	assert.Equal(t, "gpucluster-5e8a1f.unknown", unknownLeafName("gpucluster-5e8a1f"))
}

func Test_newTreeTopology_ForestOrder(t *testing.T) {
	const fabric = "gpucluster-5e8a1f"
	forest := networkForest{roots: []*networkVertex{rootVertex(fabric,
		innerVertex("t2-a0c2e8", 2, innerVertex("t1-b71e4d", 1,
			leafVertex("t0-3f9c0a", 0, testNode("worker-0", "worker", fabric), testNode("worker-1", "worker", fabric)),
			leafVertex("t0-8d2b17", 0, testNode("worker-2", "worker", fabric)),
		)),
		unknownVertex(fabric, testNode("worker-3", "worker", fabric)),
	)}}
	assert.Equal(t, TreeTopology{Switches: []Switch{
		{Name: fabric, Children: SwitchList{"t2-a0c2e8", fabric + ".unknown"}},
		{Name: "t2-a0c2e8", Children: SwitchList{"t1-b71e4d"}},
		{Name: "t1-b71e4d", Children: SwitchList{"t0-3f9c0a", "t0-8d2b17"}},
		{Name: "t0-3f9c0a", Nodes: "worker-[0-1]"},
		{Name: "t0-8d2b17", Nodes: "worker-2"},
		{Name: fabric + ".unknown", Nodes: "worker-3"},
	}}, *newTreeTopology(forest))
}

func Test_newTreeTopology_EmptyUnknown(t *testing.T) {
	forest := networkForest{roots: []*networkVertex{rootVertex("root",
		leafVertex("t0-3f9c0a", 0, testNode("worker-0", "worker", "root")),
		unknownVertex("root"),
	)}}
	assert.Equal(t, TreeTopology{Switches: []Switch{
		{Name: "root", Children: SwitchList{"t0-3f9c0a"}},
		{Name: "t0-3f9c0a", Nodes: "worker-0"},
	}}, *newTreeTopology(forest))
}

func Test_newTreeTopology_EmptySwitches(t *testing.T) {
	forest := networkForest{roots: []*networkVertex{
		rootVertex("gpucluster-1f0c",
			innerVertex("t2-90be2f", 2, innerVertex("t1-1c7d4e", 1, leafVertex("t0-a3f0b9", 0))),
			unknownVertex("gpucluster-1f0c"),
		),
		rootVertex("gpucluster-2a9e",
			leafVertex("t0-77c1e0", 0, testNode("worker-0", "worker", "gpucluster-2a9e")),
			unknownVertex("gpucluster-2a9e"),
		),
	}}
	assert.Equal(t, TreeTopology{Switches: []Switch{
		{Name: "gpucluster-2a9e", Children: SwitchList{"t0-77c1e0"}},
		{Name: "t0-77c1e0", Nodes: "worker-0"},
	}}, *newTreeTopology(forest))
}

func Test_newTreeTopology_RootOrder(t *testing.T) {
	forest := networkForest{roots: []*networkVertex{
		rootVertex("gpucluster-2a9e", leafVertex("t0-77c1e0", 0, testNode("worker-0", "worker", "gpucluster-2a9e"))),
		rootVertex("gpucluster-1f0c", leafVertex("t0-a3f0b9", 0, testNode("worker-rack0-0", "worker-rack0", "gpucluster-1f0c"))),
	}}
	assert.Equal(t, []string{"gpucluster-2a9e", "t0-77c1e0", "gpucluster-1f0c", "t0-a3f0b9"}, preOrderSwitches(*newTreeTopology(forest)))
}

func Test_newTreeTopology_EmptyForest(t *testing.T) {
	assert.Equal(t, treeTopologyPlaceholder, newTreeTopology(networkForest{}))
}

func Test_TreeTopology_changeFrom_Basic(t *testing.T) {
	tree := func(switches ...Switch) *TreeTopology { return &TreeTopology{Switches: switches} }
	previous := tree(
		Switch{Name: "gpucluster-5e8a1f", Children: SwitchList{"t2-a0c2e8", "gpucluster-5e8a1f.unknown"}},
		Switch{Name: "t2-a0c2e8", Children: SwitchList{"t1-b71e4d"}},
		Switch{Name: "t1-b71e4d", Children: SwitchList{"t0-3f9c0a", "t0-8d2b17"}},
		Switch{Name: "t0-3f9c0a", Nodes: "worker-[0-3]"},
		Switch{Name: "t0-8d2b17", Nodes: "worker-[4-6]"},
		Switch{Name: "gpucluster-5e8a1f.unknown", Nodes: "worker-7"},
	)
	withoutUnknown := tree(
		Switch{Name: "gpucluster-5e8a1f", Children: SwitchList{"t2-a0c2e8"}},
		Switch{Name: "t2-a0c2e8", Children: SwitchList{"t1-b71e4d"}},
		Switch{Name: "t1-b71e4d", Children: SwitchList{"t0-3f9c0a", "t0-8d2b17"}},
		Switch{Name: "t0-3f9c0a", Nodes: "worker-[0-3]"},
		Switch{Name: "t0-8d2b17", Nodes: "worker-[4-6]"},
	)
	tests := []struct {
		name    string
		current *TreeTopology
		want    Change
	}{
		{name: "same tree", current: previous, want: ChangeIdentical},
		{name: "children order", current: tree(
			Switch{Name: "gpucluster-5e8a1f", Children: SwitchList{"gpucluster-5e8a1f.unknown", "t2-a0c2e8"}},
			Switch{Name: "t2-a0c2e8", Children: SwitchList{"t1-b71e4d"}},
			Switch{Name: "t1-b71e4d", Children: SwitchList{"t0-8d2b17", "t0-3f9c0a"}},
			Switch{Name: "t0-3f9c0a", Nodes: "worker-[0-3]"},
			Switch{Name: "t0-8d2b17", Nodes: "worker-[4-6]"},
			Switch{Name: "gpucluster-5e8a1f.unknown", Nodes: "worker-7"},
		), want: ChangeIdentical},
		{name: "node moved between leaves", current: tree(
			Switch{Name: "gpucluster-5e8a1f", Children: SwitchList{"t2-a0c2e8", "gpucluster-5e8a1f.unknown"}},
			Switch{Name: "t2-a0c2e8", Children: SwitchList{"t1-b71e4d"}},
			Switch{Name: "t1-b71e4d", Children: SwitchList{"t0-3f9c0a", "t0-8d2b17"}},
			Switch{Name: "t0-3f9c0a", Nodes: "worker-[0-2]"},
			Switch{Name: "t0-8d2b17", Nodes: "worker-[3-6]"},
			Switch{Name: "gpucluster-5e8a1f.unknown", Nodes: "worker-7"},
		), want: ChangeRegistered},
		{name: "inner switch renamed", current: tree(
			Switch{Name: "gpucluster-5e8a1f", Children: SwitchList{"t2-e2d6c5", "gpucluster-5e8a1f.unknown"}},
			Switch{Name: "t2-e2d6c5", Children: SwitchList{"t1-b71e4d"}},
			Switch{Name: "t1-b71e4d", Children: SwitchList{"t0-3f9c0a", "t0-8d2b17"}},
			Switch{Name: "t0-3f9c0a", Nodes: "worker-[0-3]"},
			Switch{Name: "t0-8d2b17", Nodes: "worker-[4-6]"},
			Switch{Name: "gpucluster-5e8a1f.unknown", Nodes: "worker-7"},
		), want: ChangeRegistered},
		{name: "leaves swapped", current: tree(
			Switch{Name: "gpucluster-5e8a1f", Children: SwitchList{"t2-a0c2e8", "gpucluster-5e8a1f.unknown"}},
			Switch{Name: "t2-a0c2e8", Children: SwitchList{"t1-b71e4d"}},
			Switch{Name: "t1-b71e4d", Children: SwitchList{"t0-3f9c0a", "t0-8d2b17"}},
			Switch{Name: "t0-8d2b17", Nodes: "worker-[4-6]"},
			Switch{Name: "t0-3f9c0a", Nodes: "worker-[0-3]"},
			Switch{Name: "gpucluster-5e8a1f.unknown", Nodes: "worker-7"},
		), want: ChangeReordered},
		{name: "leaf appeared", current: tree(
			Switch{Name: "gpucluster-5e8a1f", Children: SwitchList{"t2-a0c2e8", "gpucluster-5e8a1f.unknown"}},
			Switch{Name: "t2-a0c2e8", Children: SwitchList{"t1-b71e4d"}},
			Switch{Name: "t1-b71e4d", Children: SwitchList{"t0-3f9c0a", "t0-8d2b17", "t0-c4e9d1"}},
			Switch{Name: "t0-3f9c0a", Nodes: "worker-[0-3]"},
			Switch{Name: "t0-8d2b17", Nodes: "worker-[4-5]"},
			Switch{Name: "t0-c4e9d1", Nodes: "worker-6"},
			Switch{Name: "gpucluster-5e8a1f.unknown", Nodes: "worker-7"},
		), want: ChangeReordered},
		{name: "leaf disappeared", current: tree(
			Switch{Name: "gpucluster-5e8a1f", Children: SwitchList{"t2-a0c2e8", "gpucluster-5e8a1f.unknown"}},
			Switch{Name: "t2-a0c2e8", Children: SwitchList{"t1-b71e4d"}},
			Switch{Name: "t1-b71e4d", Children: SwitchList{"t0-3f9c0a"}},
			Switch{Name: "t0-3f9c0a", Nodes: "worker-[0-6]"},
			Switch{Name: "gpucluster-5e8a1f.unknown", Nodes: "worker-7"},
		), want: ChangeReordered},
		{name: "unknown leaf disappeared", current: withoutUnknown, want: ChangeRegistered},
		{name: "from nothing", current: tree(), want: ChangeReordered},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.current.changeFrom(previous))
		})
	}
	assert.Equal(t, ChangeRegistered, previous.changeFrom(withoutUnknown), "unknown leaf appeared")
	assert.Equal(t, ChangeIdentical, tree().changeFrom(tree()))
}
