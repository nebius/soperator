package exporter

import "context"

const (
	nvlinkInstanceGroupLabel = "topology.nebius.com/nvl-instance-group-id"
	slurmNodeSetNameLabel    = "slurm.nebius.ai/nodeset-name"
)

// NodeTopology describes the Kubernetes placement metadata for a Slurm node.
type NodeTopology struct {
	KubernetesNode      string
	NVLinkInstanceGroup string
	SlurmNodeSetName    string
}

// NodeTopologySource resolves Slurm worker names to Kubernetes node topology.
type NodeTopologySource interface {
	ListNodeTopologies(ctx context.Context) (map[string]NodeTopology, error)
}
