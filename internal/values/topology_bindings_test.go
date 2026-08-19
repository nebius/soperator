package values

import (
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

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
		assert.Equal(t, "ib-gpu=block,everything=tree", BuildNodeSetTopologyBindings(topology, "h100"))
	})

	t.Run("a NodeSet of another fabric gets its own topology", func(t *testing.T) {
		assert.Equal(t, "eth-cpu=tree,everything=tree", BuildNodeSetTopologyBindings(topology, "cpu"))
	})

	t.Run("an unreferenced NodeSet still gets the ALL topology", func(t *testing.T) {
		assert.Equal(t, "everything=tree", BuildNodeSetTopologyBindings(topology, "unlisted"))
	})

	t.Run("empty refs cover every NodeSet", func(t *testing.T) {
		single := &slurmv1.Topology{
			Topologies: []slurmv1.NamedTopology{
				{Name: "only", Topo: slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree}},
			},
		}
		assert.Equal(t, "only=tree", BuildNodeSetTopologyBindings(single, "anything"))
	})

	t.Run("no topology section yields no bindings", func(t *testing.T) {
		assert.Empty(t, BuildNodeSetTopologyBindings(nil, "h100"))
	})

	t.Run("legacy single-topology cluster yields no bindings", func(t *testing.T) {
		assert.Empty(t, BuildNodeSetTopologyBindings(&slurmv1.Topology{}, "h100"))
	})
}
