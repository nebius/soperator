package topology

import (
	"slices"

	"nebius.ai/slurm-operator/internal/utils/slurm/nodelist"
)

// UnknownBlock names a fabric's catch-all block for nodes without a path or without tier-0:
// "unknown" for the default fabric, "<fabric>.unknown" otherwise.
const UnknownBlock = "unknown"

func unknownBlockName(fabric string) string {
	if fabric == DefaultFabric {
		return UnknownBlock
	}
	return fabric + "." + UnknownBlock
}

// BlockTopology represents Slurm topology with type "block" in topology.yaml format
type BlockTopology struct {
	BlockSizes []int `yaml:"block_sizes"`
	// Blocks are in the order they are written. Every fabric in scope has its unknown block, empty
	// or not, so a node going down never adds or removes a block.
	Blocks []Block `yaml:"blocks"`
}

type Block struct {
	Name  string            `yaml:"block"`
	Nodes nodelist.NodeList `yaml:"nodes,omitempty"`
}

// blockTopologyPlaceholder is the minimal block topology configuration that doesn't break slurmctld.
var blockTopologyPlaceholder = &BlockTopology{
	BlockSizes: []int{1},
	Blocks: []Block{{
		Name:  UnknownBlock,
		Nodes: "",
	}},
}

// newBlockTopology builds the Slurm block topology.
//
// It writes a block per tier-0 leaf holding nodes, in forest order, then the
// fabric's unknown block. Nodes on higher tiers have no block and join the unknown one.
// The unknown block is present even if it has no nodes.
func newBlockTopology(forest networkForest, blockSizes []int) *BlockTopology {
	var blocks []Block
	for _, root := range forest.roots {
		var unknown []string
		root.traverseLeaves(func(leaf *networkVertex) {
			if leaf.unknown || leaf.tier != 0 {
				unknown = append(unknown, leaf.nodeNames()...)
				return
			}
			blocks = append(blocks, Block{Name: leaf.name, Nodes: nodelist.NewNodeList(leaf.nodeNames())})
		})
		blocks = append(blocks, Block{Name: unknownBlockName(root.name), Nodes: nodelist.NewNodeList(unknown)})
	}
	if len(blocks) == 0 {
		return blockTopologyPlaceholder
	}
	return &BlockTopology{BlockSizes: blockSizes, Blocks: blocks}
}

// changeFrom is ChangeRestructured for different block sizes or a block absent from previous: a node
// registering into a block slurmctld has not loaded is drained. A different block sequence, a
// vanished block included, is ChangeReordered; different node lists are ChangeRegistered.
func (t *BlockTopology) changeFrom(previous *BlockTopology) Change {
	if !slices.Equal(t.BlockSizes, previous.BlockSizes) {
		return ChangeRestructured
	}
	previousNodes := make(map[string]nodelist.NodeList, len(previous.Blocks))
	for _, block := range previous.Blocks {
		previousNodes[block.Name] = block.Nodes
	}
	for _, block := range t.Blocks {
		if _, ok := previousNodes[block.Name]; !ok {
			return ChangeRestructured
		}
	}
	if len(t.Blocks) != len(previous.Blocks) {
		return ChangeReordered
	}
	change := ChangeIdentical
	for i, block := range t.Blocks {
		if block.Name != previous.Blocks[i].Name {
			return ChangeReordered
		}
		if block.Nodes != previousNodes[block.Name] {
			change = ChangeRegistered
		}
	}
	return change
}
