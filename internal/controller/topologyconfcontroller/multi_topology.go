package topologyconfcontroller

import (
	"context"
	"fmt"

	"k8s.io/utils/ptr"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	topologyrefs "nebius.ai/slurm-operator/internal/utils/slurm/topology"
)

// otherTopologyConfigKey returns the ConfigMap key of the mode that is not in use.
func otherTopologyConfigKey(configKey string) string {
	if configKey == consts.ConfigMapKeyTopologyYAML {
		return consts.ConfigMapKeyTopologyConfig
	}
	return consts.ConfigMapKeyTopologyYAML
}

// buildMultiTopologyYAML renders spec.topology.topologies into the body of topology.yaml.
//
// Each named topology is built by the same node/fabric discovery that feeds the single-topology
// builder, restricted to the NodeSets the topology references. Topologies may overlap: Slurm lets a
// node belong to several topologies at once, so a NodeSet described as a tree in one topology and
// as blocks in another is legitimate rather than a conflict.
func (r *WorkerTopologyReconciler) buildMultiTopologyYAML(
	ctx context.Context,
	slurmCluster *slurmv1.SlurmCluster,
	nodeSetList []v1alpha1.NodeSet,
	topologyNodeLabelsCM *corev1.ConfigMap,
	gpuPodsByNode map[string][]string,
) (string, error) {
	logger := log.FromContext(ctx).WithName(WorkerTopologyReconcilerName)

	labelsByNode, err := r.ParseNodeTopologyLabels(topologyNodeLabelsCM.Data)
	if err != nil {
		return "", fmt.Errorf("deserialize node topology labels: %w", err)
	}

	specs := namedTopologies(slurmCluster)
	entries := make([]topologyYAMLEntry, 0, len(specs))
	for _, spec := range specs {
		nodeSets := selectNodeSets(nodeSetList, spec.NodeSetRefs)
		if len(nodeSets) == 0 {
			// Emitting a tree or block topology with no nodes would hand slurmctld an empty
			// switch list; keeping the entry out is closer to "not discovered yet".
			logger.Info("Topology matches no NodeSet, skipping",
				"topology", spec.Name, "nodeSetRefs", spec.NodeSetRefs)
			continue
		}

		entry, err := buildTopologyEntry(ctx, spec, labelsByNode, gpuPodsByNode, nodeSets)
		if err != nil {
			return "", fmt.Errorf("build topology %q: %w", spec.Name, err)
		}
		entries = append(entries, entry)
	}

	markClusterDefault(ctx, entries)

	return renderTopologyYAML(entries)
}

// buildTopologyEntry turns one NamedTopology into its topology.yaml entry.
//
// The node labels a topology reads follow from its plugin: block topologies group nodes by the
// "tier-0" label, tree topologies walk the contiguous "tier-1".."tier-N" chain. Both come from the
// topology-node-labels ConfigMap that NodeTopologyReconciler builds out of the
// topologyconf.slurm.nebius.ai/tier-* labels on the Kubernetes nodes.
func buildTopologyEntry(
	ctx context.Context,
	spec slurmv1.NamedTopology,
	labelsByNode map[string]NodeTopologyLabels,
	gpuPodsByNode map[string][]string,
	nodeSets []v1alpha1.NodeSet,
) (topologyYAMLEntry, error) {
	entry := topologyYAMLEntry{
		Topology:       spec.Name,
		ClusterDefault: ptr.Deref(spec.ClusterDefault, false),
	}

	allNodeNames := collectAllNodeNames(nodeSets)
	fabricByNode := collectFabricByNode(nodeSets)
	scopedGPUPods := scopeGPUPodsToNodes(gpuPodsByNode, allNodeNames)

	switch spec.Topo.Type {
	case consts.SlurmTopologyTypeBlock:
		blocks := BuildTopologyBlocks(ctx, labelsByNode, scopedGPUPods, allNodeNames, fabricByNode)
		entry.Block = &blockTopologyYAML{
			BlockSizes: spec.Topo.BlockSizes,
			Blocks:     blocks.RenderBlocks(),
		}
	case consts.SlurmTopologyTypeTree:
		graph := BuildTopologyGraph(ctx, labelsByNode, scopedGPUPods, allNodeNames, fabricByNode)
		entry.Tree = &treeTopologyYAML{Switches: graph.RenderSwitches()}
	default:
		return topologyYAMLEntry{}, fmt.Errorf("unsupported topology type %q", spec.Topo.Type)
	}

	return entry, nil
}

// selectNodeSets returns the NodeSets a topology covers. A topology reaching none of them is left
// out of the file entirely, which is why the partition renderer applies the same rule before
// binding a partition to it. See the topology utility package.
func selectNodeSets(nodeSetList []v1alpha1.NodeSet, refs []string) []v1alpha1.NodeSet {
	return topologyrefs.SelectNodeSets(nodeSetList, refs)
}

// scopeGPUPodsToNodes drops pods that belong to NodeSets outside the topology being built, so a
// topology never places workers it does not own onto its switches or blocks.
func scopeGPUPodsToNodes(gpuPodsByNode map[string][]string, allNodeNames []string) map[string][]string {
	owned := make(map[string]struct{}, len(allNodeNames))
	for _, name := range allNodeNames {
		owned[name] = struct{}{}
	}

	scoped := make(map[string][]string, len(gpuPodsByNode))
	for node, workers := range gpuPodsByNode {
		var kept []string
		for _, worker := range workers {
			if _, ok := owned[worker]; ok {
				kept = append(kept, worker)
			}
		}
		if len(kept) > 0 {
			scoped[node] = kept
		}
	}
	return scoped
}

// markClusterDefault makes exactly one entry the cluster default.
//
// Slurm honours the first topology flagged as default, so extra flags are cleared rather than
// treated as an error. When none is flagged, the first entry is promoted.
//
// Promotion is not a convenience, it is what keeps the choice deterministic. Slurm has no
// "no cluster default" mode: it sorts the topologies so that flagged ones come first and then uses
// index 0 unconditionally, both for partitions without a Topology= and for cluster-wide operations
// such as slurmctld-to-slurmd message forwarding. With nothing flagged, index 0 is whatever the
// sort left there, so writing the default out explicitly is the only way to know which topology
// serves those operations.
func markClusterDefault(ctx context.Context, entries []topologyYAMLEntry) {
	if len(entries) == 0 {
		return
	}
	logger := log.FromContext(ctx).WithName(WorkerTopologyReconcilerName)

	defaulted := false
	for i := range entries {
		if !entries[i].ClusterDefault {
			continue
		}
		if defaulted {
			logger.Info("Several topologies request clusterDefault, clearing the extra one",
				"topology", entries[i].Topology)
			entries[i].ClusterDefault = false
			continue
		}
		defaulted = true
	}

	if defaulted {
		return
	}

	logger.Info("No topology requests clusterDefault, promoting the first one",
		"topology", entries[0].Topology)
	entries[0].ClusterDefault = true
}

// namedTopologies returns the cluster's named topologies without assuming the topology section is
// present.
func namedTopologies(slurmCluster *slurmv1.SlurmCluster) []slurmv1.NamedTopology {
	if slurmCluster.Spec.Topology == nil {
		return nil
	}
	return slurmCluster.Spec.Topology.Topologies
}
