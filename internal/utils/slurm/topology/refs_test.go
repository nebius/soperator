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
	ns.Spec.GPU.Enabled = true
	return ns
}

func cpuNodeSet() v1alpha1.NodeSet {
	ns := v1alpha1.NodeSet{}
	ns.Name = "cpu"
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

func TestSplitByGPU(t *testing.T) {
	all := []v1alpha1.NodeSet{nodeSet("h100"), cpuNodeSet(), nodeSet("h200")}

	assert.Equal(t, []v1alpha1.NodeSet{nodeSet("h100"), nodeSet("h200")}, topologyrefs.GPUNodeSets(all))
	assert.Equal(t, []v1alpha1.NodeSet{cpuNodeSet()}, topologyrefs.CPUOnlyNodeSets(all))
}

// TestRenderedNames_CPUOnly pins that CPU-only NodeSets never satisfy a user-defined topology and
// instead produce the generated flat one, which partitions may bind to.
func TestRenderedNames_CPUOnly(t *testing.T) {
	topology := &slurmv1.Topology{
		Topologies: []slurmv1.NamedTopology{
			{Name: "ib", NodeSetRefs: []string{consts.SlurmTopologyNodeSetRefAll}},
		},
	}

	t.Run("a cluster with both keeps the user topology and adds the generated one", func(t *testing.T) {
		rendered := topologyrefs.RenderedNames(topology, []v1alpha1.NodeSet{nodeSet("h100"), cpuNodeSet()})

		assert.Equal(t, map[string]struct{}{
			"ib":                            {},
			consts.SlurmTopologyCPUOnlyName: {},
		}, rendered)
	})

	t.Run("a CPU-only cluster drops the user topology", func(t *testing.T) {
		rendered := topologyrefs.RenderedNames(topology, []v1alpha1.NodeSet{cpuNodeSet()})

		assert.Equal(t, map[string]struct{}{consts.SlurmTopologyCPUOnlyName: {}}, rendered,
			"ALL must not pull CPU-only NodeSets into an IB topology")
	})

	t.Run("a GPU-only cluster generates nothing extra", func(t *testing.T) {
		rendered := topologyrefs.RenderedNames(topology, []v1alpha1.NodeSet{nodeSet("h100")})

		assert.Equal(t, map[string]struct{}{"ib": {}}, rendered)
	})
}

func TestDeclaredNames(t *testing.T) {
	topology := &slurmv1.Topology{
		Topologies: []slurmv1.NamedTopology{
			{Name: "ib", NodeSetRefs: []string{"never-applied"}},
		},
	}

	declared := topologyrefs.DeclaredNames(topology, []v1alpha1.NodeSet{cpuNodeSet()})

	assert.Equal(t, map[string]struct{}{
		"ib":                            {},
		consts.SlurmTopologyCPUOnlyName: {},
	}, declared, "a declared-but-unrendered topology stays declared")

	assert.Nil(t, topologyrefs.DeclaredNames(&slurmv1.Topology{}, []v1alpha1.NodeSet{cpuNodeSet()}),
		"a legacy cluster names no topology at all")
}
