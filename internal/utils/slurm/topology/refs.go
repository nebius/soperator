// Package topology resolves which NodeSets a named topology covers.
//
// The rule lives here because three places must agree on it: the topology controller building
// topology.yaml, the partition renderer deciding whether a Topology= binding resolves, and the
// NodeSet renderer telling a worker which topologies to register into. A topology that reaches
// none of the NodeSets is absent from the rendered config, so a partition must not bind to it.
package topology

import (
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

// CoversNodeSet reports whether a topology with the given refs claims the NodeSet. An empty ref
// list, like the explicit "ALL" ref, covers every NodeSet of the cluster.
func CoversNodeSet(refs []string, nodeSetName string) bool {
	if len(refs) == 0 {
		return true
	}
	for _, ref := range refs {
		if ref == consts.SlurmTopologyNodeSetRefAll || ref == nodeSetName {
			return true
		}
	}
	return false
}

// SelectNodeSets returns the NodeSets a topology covers. Refs that match no NodeSet are dropped
// instead of failing: they show up routinely while NodeSets and SlurmCluster are applied one after
// another, and one stale name must not take the whole topology config down.
func SelectNodeSets(nodeSets []v1alpha1.NodeSet, refs []string) []v1alpha1.NodeSet {
	if len(refs) == 0 {
		return nodeSets
	}

	var selected []v1alpha1.NodeSet
	for _, nodeSet := range nodeSets {
		if CoversNodeSet(refs, nodeSet.Name) {
			selected = append(selected, nodeSet)
		}
	}
	return selected
}

// RenderedNames returns the names of the topologies that reach at least one NodeSet, which is
// exactly the set that ends up in the rendered topology config. Topologies covering nothing are
// left out of the file, so binding a partition to one would point slurmctld at a topology it never
// sees.
func RenderedNames(topology *slurmv1.Topology, nodeSets []v1alpha1.NodeSet) map[string]struct{} {
	if topology == nil {
		return nil
	}

	names := make(map[string]struct{}, len(topology.Topologies))
	for _, named := range topology.Topologies {
		if len(SelectNodeSets(nodeSets, named.NodeSetRefs)) == 0 {
			continue
		}
		names[named.Name] = struct{}{}
	}
	return names
}
