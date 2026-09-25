package topology_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	topologyrefs "nebius.ai/slurm-operator/internal/utils/slurm/topology"
)

func cpuOnlyNodeSet(name string) v1alpha1.NodeSet {
	ns := v1alpha1.NodeSet{}
	ns.Name = name
	return ns
}

func nodeSet(name string) v1alpha1.NodeSet {
	ns := v1alpha1.NodeSet{}
	ns.Name = name
	ns.Spec.GPU.Enabled = true
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

func TestDeclaredNames(t *testing.T) {
	topology := &slurmv1.Topology{
		Topologies: []slurmv1.NamedTopology{
			{Name: "ib", NodeSetRefs: []string{"never-applied"}},
		},
	}

	declared := topologyrefs.DeclaredNames(topology)

	assert.Equal(t, map[string]struct{}{"ib": {}}, declared,
		"a declared-but-unrendered topology stays declared")

	assert.Nil(t, topologyrefs.DeclaredNames(&slurmv1.Topology{}),
		"a legacy cluster names no topology at all")
}

// TestListedNodeSets pins which NodeSets a topology actually lists, which is the rule the topology
// controller and the worker bindings must agree on.
func TestListedNodeSets(t *testing.T) {
	all := []v1alpha1.NodeSet{nodeSet("h100"), cpuOnlyNodeSet("cpu")}
	refs := []string{consts.SlurmTopologyNodeSetRefAll}

	t.Run("tree lists GPU NodeSets only", func(t *testing.T) {
		listed := topologyrefs.ListedNodeSets(all, consts.SlurmTopologyTypeTree, refs)
		assert.Equal(t, []v1alpha1.NodeSet{nodeSet("h100")}, listed,
			"a CPU-only node sits on no fabric and must not appear under a switch")
	})

	t.Run("block lists GPU NodeSets only", func(t *testing.T) {
		listed := topologyrefs.ListedNodeSets(all, consts.SlurmTopologyTypeBlock, refs)
		assert.Equal(t, []v1alpha1.NodeSet{nodeSet("h100")}, listed)
	})

	t.Run("flat keeps everything it covers", func(t *testing.T) {
		listed := topologyrefs.ListedNodeSets(all, consts.SlurmTopologyTypeFlat, refs)
		assert.Equal(t, all, listed, "flat lists no nodes, so the GPU filter does not apply")
	})

	t.Run("refs still narrow the selection", func(t *testing.T) {
		listed := topologyrefs.ListedNodeSets(all, consts.SlurmTopologyTypeTree, []string{"cpu"})
		assert.Empty(t, listed)
	})
}
