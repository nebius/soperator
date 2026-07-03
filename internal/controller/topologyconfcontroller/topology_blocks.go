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
				// Block names are external tier-0 labels; sanitize them like switch names. The
				// worker list must stay verbatim to match real Slurm node names.
				slurmSafeSwitchName(blockName),
				slurmpattern.Merge(workers),
			),
		)
	}

	return lines
}

// BuildTopologyBlocks builds the block topology in two stages, mirroring BuildTopologyGraph.
//
// Stage 1 places every Slurm node from allNodeNames into its fabric's "unknown" block, keeping
// the topology complete and stable regardless of pod lifecycle. Stage 2 overlays real blocks:
// GPU pods scheduled to a K8s node carrying a "tier-0" label (gpuPodsByNode) are moved from
// "unknown" into that block. Non-GPU nodes and unscheduled or unlabeled GPU nodes stay in
// "unknown".
//
// Blocks themselves have no root hierarchy, so real tier-0 blocks are fabric-agnostic. Only the
// catch-all "unknown" block is split per fabric (via fabricByNode, keyed by Slurm node name) so
// powered-down nodes from different fabrics don't get lumped into one block.
func BuildTopologyBlocks(
	ctx context.Context,
	labelsByNode map[string]NodeTopologyLabels,
	gpuPodsByNode map[string][]string,
	allNodeNames []string,
	fabricByNode map[string]string,
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

	// Stage 1: every node not placed into a real block goes into its fabric's "unknown" block.
	for _, name := range allNodeNames {
		if _, ok := placed[name]; ok {
			continue
		}
		blocks.AddNode(unknownSwitchName(fabricOf(fabricByNode, name)), name)
	}

	return blocks
}
