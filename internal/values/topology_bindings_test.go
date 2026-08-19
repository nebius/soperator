package values

import (
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

func gpuNodeSet(name string) *slurmv1alpha1.NodeSet {
	ns := &slurmv1alpha1.NodeSet{}
	ns.Name = name
	ns.Spec.GPU.Enabled = true
	return ns
}

func cpuNodeSet(name string) *slurmv1alpha1.NodeSet {
	ns := &slurmv1alpha1.NodeSet{}
	ns.Name = name
	return ns
}

func TestBuildNodeSetTopologyBindings(t *testing.T) {
	topology := &slurmv1.Topology{
		Topologies: []slurmv1.NamedTopology{
			{
				Name:        "ib-gpu",
				Topo:        slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeBlock},
				NodeSetRefs: []string{"h100", "h200"},
			},
			{
				Name:        "eth-cpu",
				Topo:        slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
				NodeSetRefs: []string{"cpu"},
			},
			{
				Name:        "everything",
				Topo:        slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
				NodeSetRefs: []string{consts.SlurmTopologyNodeSetRefAll},
			},
		},
	}

	t.Run("lists every topology covering the NodeSet", func(t *testing.T) {
		assert.Equal(t, "ib-gpu=block,everything=tree", BuildNodeSetTopologyBindings(topology, gpuNodeSet("h100")))
	})

	t.Run("a NodeSet of another fabric gets its own topology", func(t *testing.T) {
		assert.Equal(t, "eth-cpu=tree,everything=tree", BuildNodeSetTopologyBindings(topology, gpuNodeSet("cpu")))
	})

	t.Run("an unreferenced NodeSet still gets the ALL topology", func(t *testing.T) {
		assert.Equal(t, "everything=tree", BuildNodeSetTopologyBindings(topology, gpuNodeSet("unlisted")))
	})

	t.Run("empty refs cover every NodeSet", func(t *testing.T) {
		single := &slurmv1.Topology{
			Topologies: []slurmv1.NamedTopology{
				{Name: "only", Topo: slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree}},
			},
		}
		assert.Equal(t, "only=tree", BuildNodeSetTopologyBindings(single, gpuNodeSet("anything")))
	})

	t.Run("no topology section yields no bindings", func(t *testing.T) {
		assert.Empty(t, BuildNodeSetTopologyBindings(nil, gpuNodeSet("h100")))
	})

	t.Run("legacy single-topology cluster yields no bindings", func(t *testing.T) {
		assert.Empty(t, BuildNodeSetTopologyBindings(&slurmv1.Topology{}, gpuNodeSet("h100")))
	})
}

// TestBuildNodeSetTopologyBindings_CPUOnly pins that a CPU-only NodeSet follows the generated flat
// topology and nothing else, matching what the topology controller writes into topology.yaml.
func TestBuildNodeSetTopologyBindings_CPUOnly(t *testing.T) {
	topology := &slurmv1.Topology{
		Topologies: []slurmv1.NamedTopology{
			{
				Name:        "ib",
				Topo:        slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
				NodeSetRefs: []string{consts.SlurmTopologyNodeSetRefAll},
			},
		},
	}

	assert.Equal(t, "cpu=flat", BuildNodeSetTopologyBindings(topology, cpuNodeSet("cpu")),
		"an ALL topology must not claim a CPU-only NodeSet")
	assert.Equal(t, "ib=tree", BuildNodeSetTopologyBindings(topology, gpuNodeSet("h100")))
}
