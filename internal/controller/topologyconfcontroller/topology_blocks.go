package topologyconfcontroller

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TopologyGraph represents a block topology.
// https://slurm.schedmd.com/topology.html#block
type TopologyBlocks struct {
	blocks map[string][]string
}

func newTopologyBlocks() TopologyBlocks {
	return TopologyBlocks{
		blocks: make(map[string][]string),
	}
}

func (b TopologyBlocks) AddNode(block, worker string) {
	b.blocks[block] = append(b.blocks[block], worker)
}

// RenderConfigLines formats each populated block as a Slurm topology.conf line:
//
//	BlockName=<tier-0 label> Nodes=<comma-separated worker list>
//
// https://slurm.schedmd.com/topology.conf.html#SECTION_EXAMPLE
func (b TopologyBlocks) RenderConfigLines() []string {
	if len(b.blocks) == 0 {
		return nil
	}

	lines := make([]string, 0, len(b.blocks))

	for blockName, workers := range b.blocks {
		if len(workers) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("BlockName=%s Nodes=%s", blockName, strings.Join(workers, ",")))
	}

	return lines
}

// BuildTopologyBlocks groups worker pods by their "tier-0" node label into topology
// blocks. Nodes without the label (or pods left without a labeled node) are assigned
// to the synthetic "unknown" block so every pod is represented in the output.
func BuildTopologyBlocks(
	ctx context.Context, labelsByNode map[string]NodeTopologyLabels, podsByNode map[string][]string,
) TopologyBlocks {
	logger := log.FromContext(ctx).WithName(WorkerTopologyReconcilerName)
	blocks := newTopologyBlocks()
	podsByNode = maps.Clone(podsByNode)
	for node, labels := range labelsByNode {
		blockName, ok := labels["tier-0"]
		if !ok {
			logger.Error(nil, "missing tier-0 on node %s label for the block topology", node)
			continue
		}

		workers := podsByNode[node]
		delete(podsByNode, node)

		for _, worker := range workers {
			blocks.AddNode(blockName, worker)
		}
	}

	// Add rest of the pods for unknown blocks.
	const unknownBlockName = "unknown"
	for _, pods := range podsByNode {
		for _, worker := range pods {
			blocks.AddNode(unknownBlockName, worker)
		}
	}

	return blocks
}
