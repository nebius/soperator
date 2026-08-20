package topology_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	topologyrefs "nebius.ai/slurm-operator/internal/utils/slurm/topology"
)

func nodeSet(name string) v1alpha1.NodeSet {
	ns := v1alpha1.NodeSet{}
	ns.Name = name
	return ns
}

func TestCoversNodeSet(t *testing.T) {
	assert.True(t, topologyrefs.CoversNodeSet(nil, "h100"), "no refs covers everything")
	assert.True(t, topologyrefs.CoversNodeSet([]string{consts.SlurmTopologyNodeSetRefAll}, "h100"))
	assert.True(t, topologyrefs.CoversNodeSet([]string{"cpu", "h100"}, "h100"))
	assert.False(t, topologyrefs.CoversNodeSet([]string{"cpu"}, "h100"))
}

func TestSelectNodeSets(t *testing.T) {
	all := []v1alpha1.NodeSet{nodeSet("h100"), nodeSet("h200"), nodeSet("cpu")}

	assert.Equal(t, all, topologyrefs.SelectNodeSets(all, nil))
	assert.Equal(t, all, topologyrefs.SelectNodeSets(all, []string{consts.SlurmTopologyNodeSetRefAll}))

	selected := topologyrefs.SelectNodeSets(all, []string{"h100", "h200"})
	assert.Equal(t, []v1alpha1.NodeSet{nodeSet("h100"), nodeSet("h200")}, selected)

	assert.Empty(t, topologyrefs.SelectNodeSets(all, []string{"never-applied"}),
		"a ref matching nothing must not silently select everything")
}

// TestRenderedNames pins the rule the topology controller and the partition renderer must agree on:
// a topology reaching no NodeSet is absent from the rendered config, so nothing may bind to it.
func TestRenderedNames(t *testing.T) {
	nodeSets := []v1alpha1.NodeSet{nodeSet("h100")}
	topology := &slurmv1.Topology{
		Topologies: []slurmv1.NamedTopology{
			{Name: "covers-by-ref", NodeSetRefs: []string{"h100"}},
			{Name: "covers-by-all", NodeSetRefs: []string{consts.SlurmTopologyNodeSetRefAll}},
			{Name: "covers-by-default"},
			{Name: "ghost", NodeSetRefs: []string{"never-applied"}},
		},
	}

	rendered := topologyrefs.RenderedNames(topology, nodeSets)

	assert.Equal(t, map[string]struct{}{
		"covers-by-ref":     {},
		"covers-by-all":     {},
		"covers-by-default": {},
	}, rendered)
}

func TestRenderedNames_NoTopologySection(t *testing.T) {
	assert.Nil(t, topologyrefs.RenderedNames(nil, []v1alpha1.NodeSet{nodeSet("h100")}))
}

func TestRenderedNames_NoNodeSets(t *testing.T) {
	topology := &slurmv1.Topology{
		Topologies: []slurmv1.NamedTopology{{Name: "any"}},
	}

	assert.Empty(t, topologyrefs.RenderedNames(topology, nil),
		"with no NodeSets at all nothing reaches the rendered config")
}
