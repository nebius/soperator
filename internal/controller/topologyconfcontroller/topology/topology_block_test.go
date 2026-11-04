package topology

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func Test_unknownBlockName_Basic(t *testing.T) {
	assert.Equal(t, "unknown", unknownBlockName(DefaultFabric))
	assert.Equal(t, "gpucluster-9b4d2c.unknown", unknownBlockName("gpucluster-9b4d2c"))
}

func Test_newBlockTopology_ForestOrder(t *testing.T) {
	const fabric = "gpucluster-9b4d2c"
	forest := networkForest{roots: []*networkVertex{rootVertex(fabric,
		innerVertex("t2-71f2ab", 2,
			innerVertex("t1-5a8e02", 1, leafVertex("t0-d13c7e", 0, testNode("worker-rack2-0", "worker-rack2", fabric))),
			innerVertex("t1-c90d3f", 1,
				leafVertex("t0-0b6e95", 0, testNode("worker-rack1-0", "worker-rack1", fabric), testNode("worker-rack1-1", "worker-rack1", fabric)),
				leafVertex("t0-e4a71b", 0, testNode("worker-rack0-0", "worker-rack0", fabric)),
			),
		),
		unknownVertex(fabric, testNode("worker-rack1-17", "worker-rack1", fabric)),
	)}}
	assert.Equal(t, BlockTopology{BlockSizes: []int{18, 36}, Blocks: []Block{
		{Name: "t0-d13c7e", Nodes: "worker-rack2-0"},
		{Name: "t0-0b6e95", Nodes: "worker-rack1-[0-1]"},
		{Name: "t0-e4a71b", Nodes: "worker-rack0-0"},
		{Name: fabric + ".unknown", Nodes: "worker-rack1-17"},
	}}, *newBlockTopology(forest, []int{18, 36}))
}

func Test_newBlockTopology_HigherTierLeaves(t *testing.T) {
	// H100 nodes labelled down to their leaf switch only: a block topology has no block for
	// them, so they join the nodes without a path in the unknown block.
	const fabric = "gpucluster-2a9e"
	forest := networkForest{roots: []*networkVertex{rootVertex(fabric,
		innerVertex("t2-e2d6c5", 2, leafVertex("t1-4b9f3a", 1, testNode("worker-0", "worker", fabric), testNode("worker-1", "worker", fabric))),
		unknownVertex(fabric, testNode("worker-2", "worker", fabric)),
	)}}
	assert.Equal(t, []Block{{Name: fabric + ".unknown", Nodes: "worker-[0-2]"}}, newBlockTopology(forest, nil).Blocks)
}

func Test_newBlockTopology_UnknownBlockPerRoot(t *testing.T) {
	// The unknown block is written even when it is empty, and even for a root that has no
	// unknown vertex at all, so a node leaving or joining it never adds or removes a block.
	forest := networkForest{roots: []*networkVertex{
		rootVertex("gpucluster-1f0c",
			leafVertex("t0-a3f0b9", 0, testNode("worker-rack0-0", "worker-rack0", "gpucluster-1f0c")),
			unknownVertex("gpucluster-1f0c"),
		),
		rootVertex("gpucluster-2a9e",
			leafVertex("t0-77c1e0", 0, testNode("worker-0", "worker", "gpucluster-2a9e")),
		),
	}}
	assert.Equal(t, []Block{
		{Name: "t0-a3f0b9", Nodes: "worker-rack0-0"},
		{Name: "gpucluster-1f0c.unknown"},
		{Name: "t0-77c1e0", Nodes: "worker-0"},
		{Name: "gpucluster-2a9e.unknown"},
	}, newBlockTopology(forest, nil).Blocks)
}

func Test_newBlockTopology_BlockSizes(t *testing.T) {
	forest := networkForest{roots: []*networkVertex{rootVertex("root", leafVertex("t0-3f9c0a", 0, testNode("worker-0", "worker", "root")))}}
	assert.Equal(t, []int{18, 36, 72}, newBlockTopology(forest, []int{18, 36, 72}).BlockSizes)
}

func Test_newBlockTopology_EmptyForest(t *testing.T) {
	assert.Equal(t, blockTopologyPlaceholder, newBlockTopology(networkForest{}, []int{18}))
}

func Test_BlockTopology_MarshalsToSlurmYAML(t *testing.T) {
	out, err := yaml.Marshal(BlockTopology{BlockSizes: []int{18, 36}, Blocks: []Block{
		{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"},
		{Name: "gpucluster-9b4d2c.unknown"},
	}})
	require.NoError(t, err)
	assert.Equal(t, `block_sizes:
    - 18
    - 36
blocks:
    - block: t0-e4a71b
      nodes: worker-rack0-[0-17]
    - block: gpucluster-9b4d2c.unknown
`, string(out))
}

func Test_BlockTopology_changeFrom_Basic(t *testing.T) {
	const fabric = "gpucluster-9b4d2c"
	block := func(sizes []int, blocks ...Block) *BlockTopology {
		return &BlockTopology{BlockSizes: sizes, Blocks: blocks}
	}
	previous := block([]int{18},
		Block{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"},
		Block{Name: "t0-0b6e95", Nodes: "worker-rack1-[0-16]"},
		Block{Name: fabric + ".unknown", Nodes: "worker-rack1-17"},
	)
	tests := []struct {
		name    string
		current *BlockTopology
		want    Change
	}{
		{name: "same blocks", current: previous, want: ChangeIdentical},
		{name: "node moved between blocks", current: block([]int{18},
			Block{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"},
			Block{Name: "t0-0b6e95", Nodes: "worker-rack1-[0-17]"},
			Block{Name: fabric + ".unknown"},
		), want: ChangeRegistered},
		{name: "blocks swapped", current: block([]int{18},
			Block{Name: "t0-0b6e95", Nodes: "worker-rack1-[0-16]"},
			Block{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"},
			Block{Name: fabric + ".unknown", Nodes: "worker-rack1-17"},
		), want: ChangeReordered},
		{name: "block disappeared", current: block([]int{18},
			Block{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"},
			Block{Name: fabric + ".unknown", Nodes: "worker-rack1-[0-17]"},
		), want: ChangeReordered},
		{name: "block appeared", current: block([]int{18},
			Block{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"},
			Block{Name: "t0-0b6e95", Nodes: "worker-rack1-[0-16]"},
			Block{Name: "t0-d13c7e", Nodes: "worker-rack2-[0-17]"},
			Block{Name: fabric + ".unknown", Nodes: "worker-rack1-17"},
		), want: ChangeRestructured},
		{name: "block sizes changed", current: block([]int{18, 36},
			Block{Name: "t0-e4a71b", Nodes: "worker-rack0-[0-17]"},
			Block{Name: "t0-0b6e95", Nodes: "worker-rack1-[0-16]"},
			Block{Name: fabric + ".unknown", Nodes: "worker-rack1-17"},
		), want: ChangeRestructured},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.current.changeFrom(previous))
		})
	}
	assert.Equal(t, ChangeIdentical, block(nil).changeFrom(block(nil)))
	assert.Equal(t, ChangeRestructured, previous.changeFrom(block([]int{18})), "every block is new to an empty topology")
}
