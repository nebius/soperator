package topology

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_validateSpecs_Basic(t *testing.T) {
	assert.NoError(t, validateSpecs([]Spec{
		mustSpec("tree", "tree", true, nil, nil),
		mustSpec("flat", "flat", false, nil, nil),
	}))
	assert.EqualError(t, validateSpecs([]Spec{
		mustSpec("tree", "tree", true, nil, nil),
		mustSpec("tree", "flat", false, nil, nil),
	}), `validate topology spec "tree": name is declared twice`)
	assert.EqualError(t, validateSpecs(nil),
		"validate topology specs: expected exactly one cluster default, found 0")
	assert.EqualError(t, validateSpecs([]Spec{mustSpec("tree", "tree", false, nil, nil)}),
		"validate topology specs: expected exactly one cluster default, found 0")
	assert.EqualError(t, validateSpecs([]Spec{
		mustSpec("tree", "tree", true, nil, nil),
		mustSpec("block", "block", true, []int{18}, nil),
	}), "validate topology specs: expected exactly one cluster default, found 2")
	assert.EqualError(t, validateSpecs([]Spec{mustSpec("block", "block", true, nil, nil)}),
		`validate topology spec "block": block sizes are missing`)
}

func Test_validateNodeSets_Basic(t *testing.T) {
	assert.NoError(t, validateNodeSets(nil))
	assert.NoError(t, validateNodeSets([]NodeSet{mustNodeSet("worker", 1, "", nil), mustNodeSet("cpu", 1, "", nil)}))
	assert.EqualError(t, validateNodeSets([]NodeSet{mustNodeSet("worker", 1, "", nil), mustNodeSet("worker", 2, "", nil)}),
		`validate NodeSet "worker": name is declared twice`)
}

func Test_NewMultiTopology_OneKindEach(t *testing.T) {
	result, err := NewMultiTopology([]Spec{
		mustSpec("tree", "tree", true, nil, []string{"worker"}),
		mustSpec("block", "block", false, []int{18}, []string{"worker"}),
		mustSpec("flat", "flat", false, nil, []string{"worker"}),
	}, []NodeSet{mustNodeSet("worker", 1, "", nil)})
	require.NoError(t, err)
	require.Equal(t, []string{"tree", "block", "flat"}, topologyNames(result), "entries keep the spec order")

	tree := byName(result, "tree")
	assert.True(t, tree.ClusterDefault)
	assert.NotNil(t, tree.Tree)
	assert.Nil(t, tree.Block)
	assert.Nil(t, tree.Flat)

	block := byName(result, "block")
	assert.False(t, block.ClusterDefault)
	assert.Nil(t, block.Tree)
	assert.NotNil(t, block.Block)
	assert.Nil(t, block.Flat)

	flat := byName(result, "flat")
	assert.False(t, flat.ClusterDefault)
	assert.Nil(t, flat.Tree)
	assert.Nil(t, flat.Block)
	assert.NotNil(t, flat.Flat)
}

func Test_NewMultiTopology_InvalidSpecs(t *testing.T) {
	_, err := NewMultiTopology([]Spec{mustSpec("tree", "tree", false, nil, nil)}, nil)
	assert.EqualError(t, err, "validate topology specs: expected exactly one cluster default, found 0")
}

func Test_NewMultiTopology_DuplicateNodeSets(t *testing.T) {
	_, err := NewMultiTopology([]Spec{mustSpec("tree", "tree", true, nil, nil)},
		[]NodeSet{mustNodeSet("worker", 1, "", nil), mustNodeSet("worker", 1, "", nil)})
	assert.EqualError(t, err, `validate NodeSet "worker": name is declared twice`)
}

func Test_NewMultiTopology_ForestError(t *testing.T) {
	_, err := NewMultiTopology([]Spec{mustSpec("tree", "tree", true, nil, []string{"worker-rack0"})}, []NodeSet{
		mustNodeSet("worker-rack0", 2, "gpucluster-9b4d2c", map[string]NetworkPath{
			"worker-rack0-0": path("t0-e4a71b", "t1-c90d3f"),
			"worker-rack0-1": path("t0-e4a71b", "t1-5a8e02"),
		}),
	})
	assert.EqualError(t, err, `build network forest: add node "worker-rack0-1": segment "t0-e4a71b" has two parents, "t1-c90d3f" and "t1-5a8e02"`)
}

func Test_NewMultiTopology_UnknownNodeSetNames(t *testing.T) {
	result, err := NewMultiTopology(
		[]Spec{mustSpec("tree", "tree", true, nil, []string{"worker", "worker-rack0"})},
		[]NodeSet{mustNodeSet("worker", 1, "gpucluster-5e8a1f", rackPaths("worker", 1, path("t0-3f9c0a")))},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpucluster-5e8a1f", "t0-3f9c0a"}, preOrderSwitches(*byName(result, "tree").Tree))
}

func Test_MultiTopology_ChangeFrom_Basic(t *testing.T) {
	tree := &TreeTopology{Switches: []Switch{
		{Name: "gpucluster-5e8a1f", Children: SwitchList{"t0-3f9c0a"}},
		{Name: "t0-3f9c0a", Nodes: "worker-[0-3]"},
	}}
	treeMoved := &TreeTopology{Switches: []Switch{
		{Name: "gpucluster-5e8a1f", Children: SwitchList{"t0-3f9c0a"}},
		{Name: "t0-3f9c0a", Nodes: "worker-[0-2]"},
	}}
	block := &BlockTopology{BlockSizes: []int{18}, Blocks: []Block{{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"}, {Name: "gpucluster-9b4d2c.unknown"}}}
	blockSwapped := &BlockTopology{BlockSizes: []int{18}, Blocks: []Block{{Name: "gpucluster-9b4d2c.unknown"}, {Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"}}}
	previous := &MultiTopology{
		{Name: "tree", ClusterDefault: true, Tree: tree},
		{Name: "block", Block: block},
		{Name: "flat", Flat: newFlatTopology()},
	}
	tests := []struct {
		name    string
		current *MultiTopology
		want    Change
	}{
		{name: "same topologies", current: previous, want: ChangeIdentical},
		{name: "entry order", current: &MultiTopology{
			{Name: "flat", Flat: newFlatTopology()},
			{Name: "block", Block: block},
			{Name: "tree", ClusterDefault: true, Tree: tree},
		}, want: ChangeIdentical},
		{name: "strongest entry change wins", current: &MultiTopology{
			{Name: "tree", ClusterDefault: true, Tree: treeMoved},
			{Name: "block", Block: blockSwapped},
			{Name: "flat", Flat: newFlatTopology()},
		}, want: ChangeReordered},
		{name: "node moved in one entry", current: &MultiTopology{
			{Name: "tree", ClusterDefault: true, Tree: treeMoved},
			{Name: "block", Block: block},
			{Name: "flat", Flat: newFlatTopology()},
		}, want: ChangeRegistered},
		{name: "cluster default moved", current: &MultiTopology{
			{Name: "tree", Tree: tree},
			{Name: "block", ClusterDefault: true, Block: block},
			{Name: "flat", Flat: newFlatTopology()},
		}, want: ChangeRestructured},
		{name: "entry changed kind", current: &MultiTopology{
			{Name: "tree", ClusterDefault: true, Block: block},
			{Name: "block", Block: block},
			{Name: "flat", Flat: newFlatTopology()},
		}, want: ChangeRestructured},
		{name: "entry removed", current: &MultiTopology{
			{Name: "tree", ClusterDefault: true, Tree: tree},
			{Name: "block", Block: block},
		}, want: ChangeRestructured},
		{name: "entry added", current: &MultiTopology{
			{Name: "tree", ClusterDefault: true, Tree: tree},
			{Name: "block", Block: block},
			{Name: "flat", Flat: newFlatTopology()},
			{Name: "block-b", Block: block},
		}, want: ChangeRestructured},
		{name: "entry renamed", current: &MultiTopology{
			{Name: "tree", ClusterDefault: true, Tree: tree},
			{Name: "block-b", Block: block},
			{Name: "flat", Flat: newFlatTopology()},
		}, want: ChangeRestructured},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.current.ChangeFrom(previous))
		})
	}
	assert.Equal(t, ChangeIdentical, (&MultiTopology{}).ChangeFrom(&MultiTopology{}))
	assert.Equal(t, ChangeIdentical, (*MultiTopology)(nil).ChangeFrom(nil))
	assert.Equal(t, ChangeIdentical, (&MultiTopology{}).ChangeFrom(nil))
	assert.Equal(t, ChangeRestructured, previous.ChangeFrom(nil), "nothing published yet")
	assert.Equal(t, ChangeRestructured, (*MultiTopology)(nil).ChangeFrom(previous))
}

func Test_SlurmTopology_IsEmpty_Basic(t *testing.T) {
	flat := FlatTopology(true)
	assert.False(t, SlurmTopology{Flat: &flat}.IsEmpty())
	assert.True(t, SlurmTopology{Tree: treeTopologyPlaceholder}.IsEmpty())
	assert.True(t, SlurmTopology{Block: blockTopologyPlaceholder}.IsEmpty())
	assert.False(t, SlurmTopology{Tree: &TreeTopology{Switches: []Switch{{Name: "root.unknown", Nodes: "worker-0"}}}}.IsEmpty(),
		"a node under unknown is still a node")
	assert.False(t, SlurmTopology{Block: &BlockTopology{Blocks: []Block{{Name: "t0-a3f0b9", Nodes: "worker-rack0-[0-17]"}}}}.IsEmpty())
}

func TestHGXClusterWithTreeAndFlatTopologies(t *testing.T) {
	const fabric = "gpucluster-5e8a1f"
	paths := nodePaths("worker", 0, 3, path("t0-3f9c0a", "t1-b71e4d", "t2-a0c2e8"))
	maps.Copy(paths, nodePaths("worker", 4, 6, path("t0-8d2b17", "t1-b71e4d", "t2-a0c2e8")))
	// worker-7 is down.

	result, err := NewMultiTopology([]Spec{
		mustSpec("tree", "tree", true, nil, []string{"worker"}),
		mustSpec("flat", "flat", false, nil, []string{"cpu"}),
	}, []NodeSet{
		mustNodeSet("worker", 8, fabric, paths),
		mustNodeSet("cpu", 2, "", nil),
	})
	require.NoError(t, err)
	require.Len(t, *result, 2)

	tree := byName(result, "tree")
	assert.True(t, tree.ClusterDefault)
	require.NotNil(t, tree.Tree)
	assert.Equal(t, TreeTopology{Switches: []Switch{
		{Name: fabric, Children: SwitchList{"t2-a0c2e8", fabric + ".unknown"}},
		{Name: "t2-a0c2e8", Children: SwitchList{"t1-b71e4d"}},
		{Name: "t1-b71e4d", Children: SwitchList{"t0-3f9c0a", "t0-8d2b17"}},
		{Name: "t0-3f9c0a", Nodes: "worker-[0-3]"},
		{Name: "t0-8d2b17", Nodes: "worker-[4-6]"},
		{Name: fabric + ".unknown", Nodes: "worker-7"},
	}}, *tree.Tree)

	flat := byName(result, "flat")
	assert.False(t, flat.ClusterDefault)
	require.NotNil(t, flat.Flat)
	assert.Equal(t, FlatTopology(true), *flat.Flat)
}

func TestNVL72ClusterWithBlockTreeAndFlatTopologies(t *testing.T) {
	const fabric = "gpucluster-9b4d2c"
	racks := []string{"worker-rack0", "worker-rack1", "worker-rack2"}
	// Racks 0 and 1 share a leaf switch, rack 2 has its own; all three share the spine.
	// worker-rack1-17 and worker-rack2-3 are down.
	nodeSets := []NodeSet{
		mustNodeSet("worker-rack0", 18, fabric, rackPaths("worker-rack0", 18, path("t0-e4a71b", "t1-c90d3f", "t2-71f2ab"))),
		mustNodeSet("worker-rack1", 18, fabric, rackPaths("worker-rack1", 18, path("t0-0b6e95", "t1-c90d3f", "t2-71f2ab"), 17)),
		mustNodeSet("worker-rack2", 18, fabric, rackPaths("worker-rack2", 18, path("t0-d13c7e", "t1-5a8e02", "t2-71f2ab"), 3)),
		mustNodeSet("cpu", 4, "", nil),
	}

	result, err := NewMultiTopology([]Spec{
		mustSpec("block", "block", true, []int{18, 36}, racks),
		mustSpec("tree", "tree", false, nil, racks),
		mustSpec("flat", "flat", false, nil, []string{"cpu"}),
	}, nodeSets)
	require.NoError(t, err)

	block := byName(result, "block")
	assert.True(t, block.ClusterDefault)
	require.NotNil(t, block.Block)
	assert.Equal(t, BlockTopology{BlockSizes: []int{18, 36}, Blocks: []Block{
		{Name: "t0-d13c7e", Nodes: "worker-rack2-[0-2,4-17]"},
		{Name: "t0-0b6e95", Nodes: "worker-rack1-[0-16]"},
		{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"},
		{Name: fabric + ".unknown", Nodes: "worker-rack1-17,worker-rack2-3"},
	}}, *block.Block)

	tree := byName(result, "tree")
	require.NotNil(t, tree.Tree)
	assert.Equal(t, []string{fabric, "t2-71f2ab", "t1-5a8e02", "t0-d13c7e", "t1-c90d3f", "t0-0b6e95", "t0-e4a71b", fabric + ".unknown"},
		preOrderSwitches(*tree.Tree))
	assert.Equal(t, blockNames(*block.Block), preOrderLeaves(*tree.Tree),
		"a partition on the tree ranks nodes the way the cluster default's blocks do")

	assert.Equal(t, FlatTopology(true), *byName(result, "flat").Flat)
}

func TestMixedHGXAndNVL72Fabrics(t *testing.T) {
	// One Slurm cluster over two GPU clusters: HGX workers on one fabric, two NVL72 racks on
	// another. The tree over every GPU NodeSet is the cluster default, the block topology covers
	// the NVL72 racks only, and the flat topology covers everything including the CPU nodes.
	// worker-rack1-5 is down.
	const hgx, nvl72 = "gpucluster-2a9e", "gpucluster-1f0c"
	nodeSets := []NodeSet{
		mustNodeSet("worker", 4, hgx, rackPaths("worker", 4, path("t0-77c1e0", "t1-4b9f3a", "t2-e2d6c5"))),
		mustNodeSet("worker-rack0", 18, nvl72, rackPaths("worker-rack0", 18, path("t0-a3f0b9", "t1-1c7d4e", "t2-90be2f"))),
		mustNodeSet("worker-rack1", 18, nvl72, rackPaths("worker-rack1", 18, path("t0-f82c1a", "t1-1c7d4e", "t2-90be2f"), 5)),
		mustNodeSet("cpu", 2, "", nil),
	}
	gpu := []string{"worker", "worker-rack0", "worker-rack1"}

	result, err := NewMultiTopology([]Spec{
		mustSpec("tree", "tree", true, nil, gpu),
		mustSpec("block", "block", false, []int{18}, []string{"worker-rack0", "worker-rack1"}),
		mustSpec("flat", "flat", false, nil, append(gpu, "cpu")),
	}, nodeSets)
	require.NoError(t, err)

	tree := *byName(result, "tree").Tree
	assert.Equal(t, []string{
		nvl72, "t2-90be2f", "t1-1c7d4e", "t0-a3f0b9", "t0-f82c1a", nvl72 + ".unknown",
		hgx, "t2-e2d6c5", "t1-4b9f3a", "t0-77c1e0",
	}, preOrderSwitches(tree), "fabrics are written in name order; the HGX fabric has no node down, so no unknown leaf")
	assert.Equal(t, Switch{Name: "t1-1c7d4e", Children: SwitchList{"t0-a3f0b9", "t0-f82c1a"}}, switchNamed(tree, "t1-1c7d4e"))
	assert.Equal(t, Switch{Name: "t0-f82c1a", Nodes: "worker-rack1-[0-4,6-17]"}, switchNamed(tree, "t0-f82c1a"))
	assert.Equal(t, Switch{Name: nvl72 + ".unknown", Nodes: "worker-rack1-5"}, switchNamed(tree, nvl72+".unknown"))
	assert.Equal(t, Switch{Name: "t0-77c1e0", Nodes: "worker-[0-3]"}, switchNamed(tree, "t0-77c1e0"))

	block := *byName(result, "block").Block
	assert.Equal(t, []Block{
		{Name: "t0-a3f0b9", Nodes: "worker-rack0-[0-17]"},
		{Name: "t0-f82c1a", Nodes: "worker-rack1-[0-4,6-17]"},
		{Name: nvl72 + ".unknown", Nodes: "worker-rack1-5"},
	}, block.Blocks, "only the NVL72 fabric is in scope")
	assert.Equal(t, blockNames(block), restrictTo(preOrderLeaves(tree), blockNames(block)),
		"the NVL72 partition's blocks follow the cluster default's leaf order")

	assert.Equal(t, FlatTopology(true), *byName(result, "flat").Flat)
}

func TestDownNodesStayInEveryTopology(t *testing.T) {
	const fabric = "gpucluster-9b4d2c"
	nodeSets := []NodeSet{
		mustNodeSet("worker-rack0", 18, fabric, rackPaths("worker-rack0", 18, path("t0-e4a71b", "t1-c90d3f", "t2-71f2ab"), 2, 9)),
	}
	result, err := NewMultiTopology([]Spec{
		mustSpec("block", "block", true, []int{18}, []string{"worker-rack0"}),
		mustSpec("tree", "tree", false, nil, []string{"worker-rack0"}),
		mustSpec("flat", "flat", false, nil, []string{"worker-rack0"}),
	}, nodeSets)
	require.NoError(t, err)

	assert.Equal(t, []Block{
		{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-1,3-8,10-17]"},
		{Name: fabric + ".unknown", Nodes: "worker-rack0-[2,9]"},
	}, byName(result, "block").Block.Blocks)
	assert.Equal(t, []Switch{
		{Name: fabric, Children: SwitchList{"t2-71f2ab", fabric + ".unknown"}},
		{Name: "t2-71f2ab", Children: SwitchList{"t1-c90d3f"}},
		{Name: "t1-c90d3f", Children: SwitchList{"t0-e4a71b"}},
		{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-1,3-8,10-17]"},
		{Name: fabric + ".unknown", Nodes: "worker-rack0-[2,9]"},
	}, byName(result, "tree").Tree.Switches)
	assert.Equal(t, FlatTopology(true), *byName(result, "flat").Flat)
}

func TestTopologyDeclaredBeforeItsNodeSetsExist(t *testing.T) {
	// The NVL72 racks are declared in the block topology before their NodeSets are created.
	result, err := NewMultiTopology([]Spec{
		mustSpec("tree", "tree", true, nil, []string{"worker"}),
		mustSpec("block", "block", false, []int{18}, []string{"worker-rack0", "worker-rack1"}),
	}, []NodeSet{
		mustNodeSet("worker", 2, "gpucluster-5e8a1f", rackPaths("worker", 2, path("t0-3f9c0a", "t1-b71e4d", "t2-a0c2e8"))),
	})
	require.NoError(t, err)

	assert.Equal(t, blockTopologyPlaceholder, byName(result, "block").Block, "nothing to list yet, only the placeholder")
	assert.Equal(t, []string{"gpucluster-5e8a1f", "t2-a0c2e8", "t1-b71e4d", "t0-3f9c0a"}, preOrderSwitches(*byName(result, "tree").Tree))
}

func TestNodeSetsScaledToZero(t *testing.T) {
	const fabric = "gpucluster-9b4d2c"
	racks := []string{"worker-rack0", "worker-rack1"}
	result, err := NewMultiTopology([]Spec{
		mustSpec("block", "block", true, []int{18}, racks),
		mustSpec("tree", "tree", false, nil, racks),
		mustSpec("flat", "flat", false, nil, racks),
	}, []NodeSet{
		mustNodeSet("worker-rack0", 18, fabric, rackPaths("worker-rack0", 18, path("t0-e4a71b", "t1-c90d3f", "t2-71f2ab"))),
		mustNodeSet("worker-rack1", 0, fabric, nil),
	})
	require.NoError(t, err)
	assert.Equal(t, []Block{{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"}, {Name: fabric + ".unknown"}}, byName(result, "block").Block.Blocks)

	result, err = NewMultiTopology([]Spec{
		mustSpec("block", "block", true, []int{18}, racks),
		mustSpec("tree", "tree", false, nil, racks),
		mustSpec("flat", "flat", false, nil, racks),
	}, []NodeSet{
		mustNodeSet("worker-rack0", 0, fabric, nil),
		mustNodeSet("worker-rack1", 0, fabric, nil),
	})
	require.NoError(t, err)
	assert.Equal(t, blockTopologyPlaceholder, byName(result, "block").Block, "a cluster scaled to zero lists only placeholders")
	assert.Equal(t, treeTopologyPlaceholder, byName(result, "tree").Tree)
	assert.Equal(t, FlatTopology(true), *byName(result, "flat").Flat)
}

func TestPartialLabelsDuringRollout(t *testing.T) {
	// Half of rack 0 does not know its spine yet, and one node knows only its NVLink domain. The
	// forest learns the missing tiers from the other nodes, so the whole rack stays in one place.
	const fabric = "gpucluster-9b4d2c"
	paths := nodePaths("worker-rack0", 0, 8, path("t0-e4a71b", "t1-c90d3f", "t2-71f2ab"))
	maps.Copy(paths, nodePaths("worker-rack0", 9, 16, path("t0-e4a71b", "t1-c90d3f")))
	paths["worker-rack0-17"] = path("t0-e4a71b")
	racks := []string{"worker-rack0", "worker-rack1"}

	result, err := NewMultiTopology([]Spec{
		mustSpec("block", "block", true, []int{18}, racks),
		mustSpec("tree", "tree", false, nil, racks),
	}, []NodeSet{
		mustNodeSet("worker-rack0", 18, fabric, paths),
		mustNodeSet("worker-rack1", 18, fabric, rackPaths("worker-rack1", 18, path("t0-0b6e95", "t1-c90d3f", "t2-71f2ab"))),
	})
	require.NoError(t, err)

	assert.Equal(t, []string{fabric, "t2-71f2ab", "t1-c90d3f", "t0-0b6e95", "t0-e4a71b"}, preOrderSwitches(*byName(result, "tree").Tree))
	assert.Equal(t, []Block{
		{Name: "t0-0b6e95", Nodes: "worker-rack1-[0-17]"},
		{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"},
		{Name: fabric + ".unknown"},
	}, byName(result, "block").Block.Blocks)
}

func TestHGXNodesWithoutLeafSwitchLabels(t *testing.T) {
	// Labelled down to the spine only, HGX nodes sit on that switch in the tree. A block topology
	// over them, which production does not configure, would have nothing but unknown.
	const fabric = "gpucluster-2a9e"
	nodeSets := []NodeSet{mustNodeSet("worker", 4, fabric, rackPaths("worker", 4, path("", "t1-4b9f3a", "t2-e2d6c5")))}

	result, err := NewMultiTopology([]Spec{
		mustSpec("tree", "tree", true, nil, []string{"worker"}),
		mustSpec("block", "block", false, []int{4}, []string{"worker"}),
	}, nodeSets)
	require.NoError(t, err)

	assert.Equal(t, TreeTopology{Switches: []Switch{
		{Name: fabric, Children: SwitchList{"t2-e2d6c5"}},
		{Name: "t2-e2d6c5", Children: SwitchList{"t1-4b9f3a"}},
		{Name: "t1-4b9f3a", Nodes: "worker-[0-3]"},
	}}, *byName(result, "tree").Tree)
	assert.Equal(t, []Block{{Name: fabric + ".unknown", Nodes: "worker-[0-3]"}}, byName(result, "block").Block.Blocks)
}

func TestRelabeledNodeContradictsItsRack(t *testing.T) {
	// One node of rack 0 was relabelled with another leaf switch. Two leaves for one NVLink domain
	// cannot be real, so the whole build is refused rather than one rack silently moved.
	const fabric = "gpucluster-9b4d2c"
	paths := rackPaths("worker-rack0", 18, path("t0-e4a71b", "t1-c90d3f", "t2-71f2ab"))
	paths["worker-rack0-5"] = path("t0-e4a71b", "t1-5a8e02", "t2-71f2ab")

	_, err := NewMultiTopology(
		[]Spec{mustSpec("block", "block", true, []int{18}, []string{"worker-rack0"})},
		[]NodeSet{mustNodeSet("worker-rack0", 18, fabric, paths)},
	)
	assert.EqualError(t, err, `build network forest: add node "worker-rack0-5": segment "t0-e4a71b" has two parents, "t1-c90d3f" and "t1-5a8e02"`)
}

func TestNodeLabelledAsItsRacksLeafSwitch(t *testing.T) {
	// A node whose lowest label is the leaf switch other nodes of its rack hang under: Slurm
	// cannot write a switch holding both nodes and switches, so the build is refused.
	const fabric = "gpucluster-9b4d2c"
	paths := rackPaths("worker-rack0", 18, path("t0-e4a71b", "t1-c90d3f", "t2-71f2ab"))
	paths["worker-rack0-5"] = path("", "t1-c90d3f", "t2-71f2ab")

	_, err := NewMultiTopology(
		[]Spec{mustSpec("tree", "tree", true, nil, []string{"worker-rack0"})},
		[]NodeSet{mustNodeSet("worker-rack0", 18, fabric, paths)},
	)
	assert.EqualError(t, err, `build network forest: segment "t1-c90d3f" holds node "worker-rack0-5" and is above segment "t0-e4a71b"`)
}

func TestHeterogeneousClusterSharesLeafOrderAcrossTopologies(t *testing.T) {
	const nvl72, hgx, unlabelled = "gpucluster-1f0c", "gpucluster-2a9e", "gpucluster-3c3c"

	rack1 := nodePaths("worker-rack1", 0, 8, path("t0-f82c1a", "t1-1c7d4e", "t2-90be2f"))
	maps.Copy(rack1, nodePaths("worker-rack1", 9, 16, path("t0-f82c1a", "t1-1c7d4e")))
	rack1["worker-rack1-17"] = path("t0-f82c1a")

	hgxWorkers := nodePaths("worker", 0, 7, path("t0-77c1e0", "t1-4b9f3a", "t2-e2d6c5"))
	maps.Copy(hgxWorkers, nodePaths("worker", 8, 15, path("t0-b4d2f0", "t1-4b9f3a", "t2-e2d6c5")))
	maps.Copy(hgxWorkers, nodePaths("worker", 16, 23, path("t0-09ce3a", "t1-d7a1b3", "t2-e2d6c5")))
	delete(hgxWorkers, "worker-5")
	delete(hgxWorkers, "worker-20")

	nodeSets := []NodeSet{
		mustNodeSet("worker-rack0", 18, nvl72, rackPaths("worker-rack0", 18, path("t0-a3f0b9", "t1-1c7d4e", "t2-90be2f"), 7)),
		mustNodeSet("worker-rack1", 18, nvl72, rack1),
		mustNodeSet("worker-rack2", 18, nvl72, rackPaths("worker-rack2", 18, path("t0-d13c7e", "t1-5a8e02", "t2-90be2f", "t3-7b2e4c"))),
		mustNodeSet("worker-rack3", 18, nvl72, nil),
		mustNodeSet("worker-rack4", 18, nvl72, rackPaths("worker-rack4", 18, path("t0-6e1a9d", "t1-e9f0a1"))),
		mustNodeSet("worker", 24, hgx, hgxWorkers),
		mustNodeSet("worker-b", 8, hgx, rackPaths("worker-b", 8, path("t0-52ee71", "t1-d7a1b3", "t2-e2d6c5"))),
		mustNodeSet("worker-nolabels", 4, unlabelled, nil),
		mustNodeSet("cpu", 3, "", nil),
		mustNodeSet("cpu-large", 0, "", nil),
	}
	racks := []string{"worker-rack0", "worker-rack1", "worker-rack2", "worker-rack3", "worker-rack4"}
	gpu := append(slices.Clone(racks), "worker", "worker-b", "worker-nolabels")
	all := append(slices.Clone(gpu), "cpu", "cpu-large")
	subset := []string{"worker-rack1", "worker-rack2", "worker-rack4"}

	result, err := NewMultiTopology([]Spec{
		mustSpec("tree", "tree", true, nil, gpu),
		mustSpec("block", "block", false, []int{18, 36}, racks),
		mustSpec("tree-nvl72", "tree", false, nil, racks),
		mustSpec("block-subset", "block", false, []int{18}, subset),
		mustSpec("tree-subset", "tree", false, nil, subset),
		mustSpec("block-declared", "block", false, []int{18}, []string{"worker-rack5"}),
		mustSpec("flat", "flat", false, nil, all),
	}, nodeSets)
	require.NoError(t, err)

	tree := *byName(result, "tree").Tree
	assert.Equal(t, []string{
		nvl72, "t3-7b2e4c", "t2-90be2f", "t1-1c7d4e", "t0-a3f0b9", "t0-f82c1a", "t1-5a8e02", "t0-d13c7e", "t1-e9f0a1", "t0-6e1a9d", nvl72 + ".unknown",
		hgx, "t2-e2d6c5", "t1-4b9f3a", "t0-77c1e0", "t0-b4d2f0", "t1-d7a1b3", "t0-09ce3a", "t0-52ee71", hgx + ".unknown",
		unlabelled, unlabelled + ".unknown",
	}, preOrderSwitches(tree))

	blocks := []Block{
		{Name: "t0-a3f0b9", Nodes: "worker-rack0-[0-6,8-17]"},
		{Name: "t0-f82c1a", Nodes: "worker-rack1-[0-17]"},
		{Name: "t0-d13c7e", Nodes: "worker-rack2-[0-17]"},
		{Name: "t0-6e1a9d", Nodes: "worker-rack4-[0-17]"},
		{Name: nvl72 + ".unknown", Nodes: "worker-rack0-7,worker-rack3-[0-17]"},
	}
	assert.Equal(t, BlockTopology{BlockSizes: []int{18, 36}, Blocks: blocks}, *byName(result, "block").Block)
	assert.Equal(t, blockNames(*byName(result, "block").Block), restrictTo(preOrderLeaves(tree), blockNames(*byName(result, "block").Block)),
		"the cluster default's leaves and the block partition's blocks share one order")
	assert.Equal(t, blockNames(*byName(result, "block").Block), preOrderLeaves(*byName(result, "tree-nvl72").Tree),
		"a tree and a block topology over the same NodeSets agree leaf by leaf")

	subBlocks := blockNames(*byName(result, "block-subset").Block)
	assert.Equal(t, []string{"t0-f82c1a", "t0-d13c7e", "t0-6e1a9d", nvl72 + ".unknown"}, subBlocks)
	assert.Equal(t, Block{Name: nvl72 + ".unknown"}, byName(result, "block-subset").Block.Blocks[3], "no scoped node is down, the unknown block stays, empty")
	assert.Equal(t, []string{"t0-f82c1a", "t0-d13c7e", "t0-6e1a9d"}, preOrderLeaves(*byName(result, "tree-subset").Tree),
		"the tree drops the empty unknown leaf but keeps the order")
	assert.Equal(t, restrictTo(blockNames(*byName(result, "block").Block), subBlocks), subBlocks,
		"a topology over fewer racks keeps the cluster-wide order")

	assert.Equal(t, blockTopologyPlaceholder, byName(result, "block-declared").Block)

	assert.Equal(t, FlatTopology(true), *byName(result, "flat").Flat)
}

func TestChangeAfterRealWorldEvents(t *testing.T) {
	const hgx, otherHGX = "gpucluster-2a9e", "gpucluster-3c3c"
	cpuOnly := scenarioCluster{cpu: 8}
	hgxCluster := func(size uint, down ...uint) scenarioCluster {
		return scenarioCluster{cpu: 8, hgx: []scenarioNodeSet{hgxNodeSet("worker", hgx, size, down...)}}
	}
	nvl72Cluster := func(racks ...scenarioNodeSet) scenarioCluster {
		return scenarioCluster{cpu: 8, racks: racks}
	}

	tests := []struct {
		name          string
		before, after scenarioCluster
		want          Change
	}{
		{
			name:   "add an HGX NodeSet to a CPU-only cluster, a tree topology appears: Restructured",
			before: cpuOnly,
			after:  hgxCluster(16),
			want:   ChangeRestructured,
		},
		{
			name:   "add an HGX NodeSet from another fabric, a second root in the tree: Reordered",
			before: hgxCluster(16),
			after:  scenarioCluster{cpu: 8, hgx: []scenarioNodeSet{hgxNodeSet("worker", hgx, 16), hgxNodeSet("worker-b", otherHGX, 8)}},
			want:   ChangeReordered,
		},
		{
			name:   "add an NVL72 rack NodeSet to an NVL72 cluster, a new block: Restructured",
			before: nvl72Cluster(nvl72Rack(0, 18)),
			after:  nvl72Cluster(nvl72Rack(0, 18), nvl72Rack(1, 18)),
			want:   ChangeRestructured,
		},
		{
			name:   "scale up an HGX NodeSet, the new nodes land under leaf switches already in the tree: Registered",
			before: hgxCluster(24),
			after:  hgxCluster(32),
			want:   ChangeRegistered,
		},
		{
			name:   "scale up an HGX NodeSet, the new nodes land under a new leaf switch: Reordered",
			before: hgxCluster(32),
			after:  hgxCluster(48),
			want:   ChangeReordered,
		},
		{
			name:   "scale down an HGX NodeSet, its leaf switches stay in the tree: Registered",
			before: hgxCluster(32),
			after:  hgxCluster(24),
			want:   ChangeRegistered,
		},
		{
			name:   "scale down an HGX NodeSet, a leaf switch leaves the tree: Reordered",
			before: hgxCluster(48),
			after:  hgxCluster(32),
			want:   ChangeReordered,
		},
		{
			name:   "power down an HGX node, the fabric's unknown leaf appears: Registered",
			before: hgxCluster(16),
			after:  hgxCluster(16, 3),
			want:   ChangeRegistered,
		},
		{
			name:   "power down an HGX node while another is already down: Registered",
			before: hgxCluster(16, 3),
			after:  hgxCluster(16, 3, 9),
			want:   ChangeRegistered,
		},
		{
			name:   "power up an HGX node under a leaf switch already in the tree, another stays down: Registered",
			before: hgxCluster(16, 3, 9),
			after:  hgxCluster(16, 3),
			want:   ChangeRegistered,
		},
		{
			name:   "power up the last down HGX node, the fabric's unknown leaf disappears: Registered",
			before: hgxCluster(16, 3),
			after:  hgxCluster(16),
			want:   ChangeRegistered,
		},
		{
			name:   "power up an HGX node under a leaf switch new to the tree: Reordered",
			before: hgxCluster(16, 3, 9),
			after: scenarioCluster{cpu: 8, hgx: []scenarioNodeSet{
				hgxNodeSet("worker", hgx, 16, 9).withPath("worker-3", path("t0-09ce3a", "t1-d7a1b3", "t2-e2d6c5")),
			}},
			want: ChangeReordered,
		},
		{
			name:   "change the number of nodes per NVL72 rack, block sizes follow: Restructured",
			before: nvl72Cluster(nvl72Rack(0, 18), nvl72Rack(1, 18)),
			after: scenarioCluster{cpu: 8, racks: []scenarioNodeSet{nvl72Rack(0, 9), nvl72Rack(1, 9)},
				blockSizes: []int{9, 18}},
			want: ChangeRestructured,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.after.topologies(t).ChangeFrom(tt.before.topologies(t)))
		})
	}
}
