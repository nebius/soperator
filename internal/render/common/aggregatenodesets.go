package common

import (
	"cmp"
	"fmt"
	"slices"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/values"
)

// aggregateSlurmNodeSet is a Slurm NodeSet gathering the nodes of every NodeSet that lists it in
// [slurmv1alpha1.NodeSetSpec.AggregateSlurmNodeSets].
type aggregateSlurmNodeSet struct {
	Name    string
	Members []slurmv1alpha1.NodeSet
}

// Replicas is the number of nodes the aggregate renders in slurm.conf.
func (a aggregateSlurmNodeSet) Replicas() int32 {
	var res int32
	for _, member := range a.Members {
		res += max(member.Spec.Replicas, 0)
	}
	return res
}

// groupAggregateSlurmNodeSets groups the cluster NodeSets by the aggregate Slurm NodeSets they list.
// Aggregates are sorted by name, members keep the order of the cluster NodeSets.
//
// Names matching an existing NodeSet are returned separately as shadowed, since rendering both would
// produce two `NodeSet=` lines with the same name.
func groupAggregateSlurmNodeSets(cluster *values.SlurmCluster) (aggregates []aggregateSlurmNodeSet, shadowed []string) {
	nodeSetNames := make(map[string]struct{}, len(cluster.NodeSets))
	for _, nodeSet := range cluster.NodeSets {
		nodeSetNames[nodeSet.Name] = struct{}{}
	}

	members := map[string][]slurmv1alpha1.NodeSet{}
	for _, nodeSet := range cluster.NodeSets {
		for _, name := range nodeSet.Spec.AggregateSlurmNodeSets {
			if _, found := nodeSetNames[name]; found {
				if !slices.Contains(shadowed, name) {
					shadowed = append(shadowed, name)
				}
				continue
			}
			members[name] = append(members[name], nodeSet)
		}
	}

	for name, nodeSets := range members {
		aggregates = append(aggregates, aggregateSlurmNodeSet{Name: name, Members: nodeSets})
	}
	slices.SortFunc(aggregates, func(a, b aggregateSlurmNodeSet) int {
		return cmp.Compare(a.Name, b.Name)
	})
	slices.Sort(shadowed)

	return aggregates, shadowed
}

// nodeSetHostlist renders the node names of the NodeSet as a Slurm hostlist expression,
// or an empty string when the NodeSet has no replicas.
func nodeSetHostlist(nodeSet slurmv1alpha1.NodeSet) string {
	switch {
	case nodeSet.Spec.Replicas == 1:
		return fmt.Sprintf("%s-0", nodeSet.Name)
	case nodeSet.Spec.Replicas > 1:
		return fmt.Sprintf("%s-[0-%d]", nodeSet.Name, nodeSet.Spec.Replicas-1)
	default:
		return ""
	}
}
