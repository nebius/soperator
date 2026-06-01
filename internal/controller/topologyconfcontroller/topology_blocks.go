package topologyconfcontroller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmpattern "nebius.ai/slurm-operator/internal/utils/slurm/pattern"
)

// TopologyBlocks represents a block topology.
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
		lines = append(
			lines,
			fmt.Sprintf(
				"BlockName=%s Nodes=%s",
				blockName,
				slurmpattern.Merge(workers),
			),
		)
	}

	return lines
}

// BuildTopologyBlocks builds the block topology in two stages, mirroring BuildTopologyGraph.
//
// Stage 1 places every Slurm node from allNodeNames into the synthetic "unknown" block, keeping
// the topology complete and stable regardless of pod lifecycle. Stage 2 overlays real blocks:
// GPU pods scheduled to a K8s node carrying a "tier-0" label (gpuPodsByNode) are moved from
// "unknown" into that block. Non-GPU nodes and unscheduled or unlabeled GPU nodes stay in
// "unknown".
func BuildTopologyBlocks(
	ctx context.Context,
	labelsByNode map[string]NodeTopologyLabels,
	gpuPodsByNode map[string][]string,
	allNodeNames []string,
) TopologyBlocks {
	logger := log.FromContext(ctx).WithName(WorkerTopologyReconcilerName)
	blocks := newTopologyBlocks()

	// Stage 2: place scheduled GPU pods into their tier-0 block.
	placed := make(map[string]struct{})
	for node, labels := range labelsByNode {
		workers := gpuPodsByNode[node]
		if len(workers) == 0 {
			continue
		}

		blockName, ok := labels["tier-0"]
		if !ok {
			// Pods fall back to the "unknown" block via stage 1.
			logger.Error(nil, "missing tier-0 label for the block topology", "node", node)
			continue
		}

		for _, worker := range workers {
			blocks.AddNode(blockName, worker)
			placed[worker] = struct{}{}
		}
	}

	// Stage 1: every node not placed into a real block goes into "unknown".
	const unknownBlockName = "unknown"
	for _, name := range allNodeNames {
		if _, ok := placed[name]; ok {
			continue
		}
		blocks.AddNode(unknownBlockName, name)
	}

	return blocks
}
