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
	entries := make([]topologyYAMLEntry, 0, len(specs)+1)
	for _, spec := range specs {
		nodeSets := topologyrefs.ListedNodeSets(nodeSetList, spec.Topo.Type, spec.NodeSetRefs)
		// Counted in nodes, not NodeSets: a NodeSet scaled to zero is present but contributes no
		// node, and building from it would emit "blocks: []" or "switches: []" -- the shapes
		// emptyTopologyEntry exists to avoid, one of which makes slurmctld fatal on startup.
		if len(collectAllNodeNames(nodeSets)) == 0 {
			// Declared but not populated yet: its NodeSets may not exist, may be scaled to zero, or
			// their pods may not be scheduled. It is still emitted so a partition bound to it keeps
			// resolving.
			logger.Info("Topology reaches no node yet, rendering it empty",
				"topology", spec.Name, "nodeSetRefs", spec.NodeSetRefs)
			// Not reported for flat: it lists no nodes by design, so reaching none is its normal
			// state rather than a misconfiguration.
			if spec.Topo.Type != consts.SlurmTopologyTypeFlat {
				r.recordTopologyIssue(slurmCluster, reasonTopologyReachesNoNode,
					"Topology %q reaches no node and is rendered empty: check its nodeSetRefs %v",
					spec.Name, spec.NodeSetRefs)
			}
			entries = append(entries, emptyTopologyEntry(spec))
			continue
		}

		entry, err := buildTopologyEntry(ctx, spec, labelsByNode, gpuPodsByNode, nodeSets)
		if err != nil {
			return "", fmt.Errorf("build topology %q: %w", spec.Name, err)
		}
		entries = append(entries, entry)
	}

	r.markClusterDefault(ctx, slurmCluster, entries)
	r.reportUnresolvedTopologyRefs(slurmCluster, entries)

	return renderTopologyYAML(entries)
}

// buildTopologyEntry turns one NamedTopology into its topology.yaml entry.
//
// The node labels a topology reads follow from its plugin: block topologies group nodes by the
// "tier-0" label, tree topologies walk the contiguous "tier-1".."tier-N" chain. Both come from the
// topology-node-labels ConfigMap that NodeTopologyReconciler builds out of the
// topology.nebius.com/tier-* labels on the Kubernetes nodes.
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
	case consts.SlurmTopologyTypeFlat:
		// A flat topology carries no nodes: it states that there is nothing to optimize, so the
		// NodeSets it covers only decide which workers skip topology registration.
		entry.Flat = true
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
func (r *WorkerTopologyReconciler) markClusterDefault(
	ctx context.Context, slurmCluster *slurmv1.SlurmCluster, entries []topologyYAMLEntry,
) {
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
			r.recordTopologyIssue(slurmCluster, reasonClusterDefaultConflict,
				"Several topologies request clusterDefault; %q was cleared and %q stays the default",
				entries[i].Topology, entries[0].Topology)
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

// placeholderBlockSizes is used when a block topology has no nodes yet and the cluster configured no
// sizes. Slurm derives its base block size from the sizes or from the first block that has nodes,
// and fatals with "Blocks do not contain any nodes and the BlockSizes are not set" when it gets
// neither. The value is irrelevant while the topology is empty and is replaced as soon as it is not.
const placeholderBlockSizes = 1

// emptyTopologyEntry renders a declared topology that reaches no node yet.
//
// It is emitted rather than skipped: a partition bound to a topology missing from the file makes
// slurmctld reject the whole config, and NodeSets routinely lag behind the SlurmCluster. The shape
// is the smallest one Slurm accepts -- an empty switch or block list is fatal, so each carries a
// single named, empty member.
func emptyTopologyEntry(spec slurmv1.NamedTopology) topologyYAMLEntry {
	entry := topologyYAMLEntry{
		Topology:       spec.Name,
		ClusterDefault: ptr.Deref(spec.ClusterDefault, false),
	}

	switch spec.Topo.Type {
	case consts.SlurmTopologyTypeFlat:
		entry.Flat = true
	case consts.SlurmTopologyTypeBlock:
		blockSizes := spec.Topo.BlockSizes
		if len(blockSizes) == 0 {
			blockSizes = []int{placeholderBlockSizes}
		}
		entry.Block = &blockTopologyYAML{
			BlockSizes: blockSizes,
			Blocks:     []blockYAML{{Block: unknownSwitchName(defaultFabric)}},
		}
	default:
		entry.Tree = &treeTopologyYAML{
			Switches: []switchYAML{{Switch: consts.SlurmTopologyDefaultFabric}},
		}
	}

	return entry
}

// reportUnresolvedTopologyRefs reports partitions bound to a topology that is not in the rendered
// config. slurmctld rejects a partition pointing at a topology it cannot see, so the operator drops
// the binding -- silently, from the cluster's point of view, until a job fails to schedule.
func (r *WorkerTopologyReconciler) reportUnresolvedTopologyRefs(
	slurmCluster *slurmv1.SlurmCluster, entries []topologyYAMLEntry,
) {
	rendered := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		rendered[entry.Topology] = struct{}{}
	}

	for _, partition := range slurmCluster.Spec.PartitionConfiguration.Partitions {
		if partition.TopologyRef == "" {
			continue
		}
		if _, ok := rendered[partition.TopologyRef]; ok {
			continue
		}
		r.recordTopologyIssue(slurmCluster, reasonUnresolvedTopologyRef,
			"Partition %q references topology %q, which is not in the rendered config; "+
				"the binding is dropped and the partition falls back to the cluster default",
			partition.Name, partition.TopologyRef)
	}
}
