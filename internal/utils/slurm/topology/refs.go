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

func GPUNodeSets(nodeSets []v1alpha1.NodeSet) []v1alpha1.NodeSet {
	var gpu []v1alpha1.NodeSet
	for _, nodeSet := range nodeSets {
		if nodeSet.Spec.GPU.Enabled {
			gpu = append(gpu, nodeSet)
		}
	}
	return gpu
}

// CPUOnlyNodeSets returns the NodeSets without GPUs.
func CPUOnlyNodeSets(nodeSets []v1alpha1.NodeSet) []v1alpha1.NodeSet {
	var cpu []v1alpha1.NodeSet
	for _, nodeSet := range nodeSets {
		if !nodeSet.Spec.GPU.Enabled {
			cpu = append(cpu, nodeSet)
		}
	}
	return cpu
}

// ListedNodeSets returns the NodeSets a topology of the given type actually lists.
//
// Tree and block topologies describe a fabric, so they list only GPU NodeSets: a CPU-only node sits
// on no fabric, and placing it under a switch or in a block would tell the scheduler it is one hop
// from the GPU nodes. A flat topology lists no nodes at all, so the distinction does not apply --
// it covers whatever partition reaches it, CPU-only NodeSets included.
func ListedNodeSets(nodeSets []v1alpha1.NodeSet, topoType string, refs []string) []v1alpha1.NodeSet {
	selected := SelectNodeSets(nodeSets, refs)
	if topoType == consts.SlurmTopologyTypeFlat {
		return selected
	}

	var gpu []v1alpha1.NodeSet
	for _, nodeSet := range selected {
		if nodeSet.Spec.GPU.Enabled {
			gpu = append(gpu, nodeSet)
		}
	}
	return gpu
}

// DeclaredNames returns every topology name the config declares. It separates "you named something that does not exist" from "you named a topology that will
// not be in the rendered config".
func DeclaredNames(topology *slurmv1.Topology) map[string]struct{} {
	if topology == nil || len(topology.Topologies) == 0 {
		return nil
	}

	names := make(map[string]struct{}, len(topology.Topologies))
	for _, named := range topology.Topologies {
		names[named.Name] = struct{}{}
	}
	return names
}
