package common

import (
	"fmt"
	"strings"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/values"
)

// Reasons why a partition's nodeSetRef doesn't get into the rendered slurm.conf.
const (
	NodeSetRefIgnoreReasonNotFound   = "NodeSet does not exist"
	NodeSetRefIgnoreReasonNoReplicas = "NodeSet has no replicas"
)

// IgnoredNodeSetRef is a partition's nodeSetRef left out of the rendered slurm.conf.
type IgnoredNodeSetRef struct {
	Partition string
	NodeSet   string
	Reason    string
}

// NodeSetRefResolution describes which partition nodeSetRefs didn't make it into slurm.conf.
type NodeSetRefResolution struct {
	Ignored []IgnoredNodeSetRef
	// PartitionsWithoutNodes are partitions that lost all of their nodeSetRefs and are therefore
	// rendered with a blank node list.
	PartitionsWithoutNodes []string
}

func (r NodeSetRefResolution) IsEmpty() bool {
	return len(r.Ignored) == 0 && len(r.PartitionsWithoutNodes) == 0
}

// ResolveNodeSetRefs reports the partition nodeSetRefs that rendering leaves out of slurm.conf.
// It repeats the decisions made by [AddPartitionsToSlurmConfig] so that the cluster status can
// explain them.
func ResolveNodeSetRefs(cluster *values.SlurmCluster) NodeSetRefResolution {
	// Only the structured config type renders partitions out of nodeSetRefs. Other types may still
	// carry a leftover partition list that never reaches slurm.conf.
	if cluster.PartitionConfiguration.ConfigType != slurmv1.PartitionConfigTypeStructured {
		return NodeSetRefResolution{}
	}

	replicas := nodeSetReplicas(cluster)

	var res NodeSetRefResolution
	for _, partition := range cluster.PartitionConfiguration.Partitions {
		if partition.IsAll || len(partition.NodeSetRefs) == 0 {
			continue
		}

		resolved, ignored := resolvePartitionNodeSetRefs(partition, replicas)
		res.Ignored = append(res.Ignored, ignored...)
		if len(resolved) == 0 {
			res.PartitionsWithoutNodes = append(res.PartitionsWithoutNodes, partition.Name)
		}
	}

	return res
}

// FormatIgnoredNodeSetRefs renders the resolution into a human-readable message, replacing the
// tail that doesn't fit into limit bytes with "and N more".
func FormatIgnoredNodeSetRefs(resolution NodeSetRefResolution, limit int) string {
	var entries []string
	for _, ref := range resolution.Ignored {
		entries = append(entries, fmt.Sprintf(
			"partition %q: nodeSetRef %q ignored (%s)", ref.Partition, ref.NodeSet, ref.Reason,
		))
	}
	for _, partition := range resolution.PartitionsWithoutNodes {
		entries = append(entries, fmt.Sprintf(
			"partition %q has no nodes: none of its nodeSetRefs are usable", partition,
		))
	}

	return joinWithinLimit(entries, limit)
}

// resolvePartitionNodeSetRefs splits a partition's nodeSetRefs into the ones that can be rendered
// and the ones that can't.
//
// A ref is renderable only when its NodeSet reaches the `NodeSet=` section of slurm.conf, i.e. the
// NodeSet exists and [AddNodeSetsToSlurmConfig] emits a line for it, which it does for a positive
// replica count only. Referring to a NodeSet missing from that section makes slurmctld reject the
// whole config.
func resolvePartitionNodeSetRefs(
	partition slurmv1.Partition,
	replicas map[string]int32,
) ([]string, []IgnoredNodeSetRef) {
	var resolved []string
	var ignored []IgnoredNodeSetRef

	for _, ref := range partition.NodeSetRefs {
		nodeSetReplicas, found := replicas[ref]
		switch {
		case !found:
			ignored = append(ignored, IgnoredNodeSetRef{
				Partition: partition.Name,
				NodeSet:   ref,
				Reason:    NodeSetRefIgnoreReasonNotFound,
			})
		case nodeSetReplicas <= 0:
			ignored = append(ignored, IgnoredNodeSetRef{
				Partition: partition.Name,
				NodeSet:   ref,
				Reason:    NodeSetRefIgnoreReasonNoReplicas,
			})
		default:
			resolved = append(resolved, ref)
		}
	}

	return resolved, ignored
}

func nodeSetReplicas(cluster *values.SlurmCluster) map[string]int32 {
	res := make(map[string]int32, len(cluster.NodeSets))
	for _, nodeSet := range cluster.NodeSets {
		res[nodeSet.Name] = nodeSet.Spec.Replicas
	}

	return res
}

func joinWithinLimit(entries []string, limit int) string {
	var res strings.Builder

	appendPart := func(part string) {
		if res.Len() > 0 {
			res.WriteString("; ")
		}
		res.WriteString(part)
	}

	for i, entry := range entries {
		tail := fmt.Sprintf("and %d more", len(entries)-i)
		// Reserve room for both separators so the tail always fits.
		if res.Len()+len("; ")+len(entry)+len("; ")+len(tail) > limit {
			appendPart(tail)
			break
		}
		appendPart(entry)
	}

	return res.String()
}
