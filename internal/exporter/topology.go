package exporter

import "context"

const nvlinkInstanceGroupLabel = "topology.nebius.com/nvl-instance-group-id"

// NodeTopology describes Soperator NodeSet membership and Kubernetes placement metadata for a Slurm node.
type NodeTopology struct {
	KubernetesNode      string
	NVLinkInstanceGroup string
	SlurmNodeSetName    string
}

// NodeTopologySource resolves Slurm worker names to Soperator NodeSet and Kubernetes node topology.
type NodeTopologySource interface {
	ListNodeTopologies(ctx context.Context) (map[string]NodeTopology, error)
}
