// Package topology resolves which NodeSets a named topology covers.
//
// The rule lives here because three places must agree on it: the topology controller building
// topology.yaml, the partition renderer deciding whether a Topology= binding resolves, and the
// NodeSet renderer telling a worker which topologies to register into. A topology that reaches
// none of the NodeSets is absent from the rendered config, so a partition must not bind to it.
package topology

import (
	"slices"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

// coversNodeSet reports whether a topology with the given refs claims the NodeSet. The explicit
// "ALL" ref covers every NodeSet of the cluster.
//
// A ref matches a NodeSet by its name or by one of its aggregate Slurm NodeSets. Like in slurm.conf,
// an aggregate name that is also the name of an existing NodeSet refers to that NodeSet only.
func coversNodeSet(refs []string, nodeSet v1alpha1.NodeSet, nodeSetNames map[string]struct{}) bool {
	for _, ref := range refs {
		if ref == consts.SlurmTopologyNodeSetRefAll || ref == nodeSet.Name {
			return true
		}
		if _, shadowed := nodeSetNames[ref]; !shadowed && slices.Contains(nodeSet.Spec.AggregateSlurmNodeSets, ref) {
			return true
		}
	}
	return false
}

// SelectNodeSets returns the NodeSets a topology covers. An empty ref list covers every NodeSet.
// Refs that match no NodeSet are dropped instead of failing: they show up routinely while NodeSets
// and SlurmCluster are applied one after another, and one stale name must not take the whole
// topology config down.
func SelectNodeSets(nodeSets []v1alpha1.NodeSet, refs []string) []v1alpha1.NodeSet {
	if len(refs) == 0 {
		return nodeSets
	}

	nodeSetNames := make(map[string]struct{}, len(nodeSets))
	for _, nodeSet := range nodeSets {
		nodeSetNames[nodeSet.Name] = struct{}{}
	}

	var selected []v1alpha1.NodeSet
	for _, nodeSet := range nodeSets {
		if coversNodeSet(refs, nodeSet, nodeSetNames) {
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
