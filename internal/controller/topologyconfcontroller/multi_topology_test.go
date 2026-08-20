package topologyconfcontroller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

func nodeSet(name string, gpu bool) v1alpha1.NodeSet {
	ns := v1alpha1.NodeSet{}
	ns.Name = name
	ns.Spec.Replicas = 2
	ns.Spec.GPU.Enabled = gpu
	return ns
}

func TestRenderTopologyYAML_MatchesSlurmSchema(t *testing.T) {
	out, err := renderTopologyYAML([]topologyYAMLEntry{
		{Topology: "topo1", ClusterDefault: true, Tree: &treeTopologyYAML{Switches: []switchYAML{
			{Switch: "root", Children: "s[1-2]"},
			{Switch: "s1", Nodes: "worker-cpu-[0-3]"},
		}}},
		{Topology: "topo2", Block: &blockTopologyYAML{BlockSizes: []int{4, 16}, Blocks: []blockYAML{
			{Block: "b1", Nodes: "worker-gpu-[0-7]"},
		}}},
	})
	require.NoError(t, err)

	// "topology" must be the first attribute of every entry, and each entry carries exactly one
	// plugin section. https://slurm.schedmd.com/topology.yaml.html
	assert.Equal(t, `- topology: topo1
  cluster_default: true
  tree:
    switches:
        - switch: root
          children: s[1-2]
        - switch: s1
          nodes: worker-cpu-[0-3]
- topology: topo2
  cluster_default: false
  block:
    block_sizes:
        - 4
        - 16
    blocks:
        - block: b1
          nodes: worker-gpu-[0-7]
`, out)
}

func TestRenderTopologyYAML_Empty(t *testing.T) {
	out, err := renderTopologyYAML(nil)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestSelectNodeSets(t *testing.T) {
	all := []v1alpha1.NodeSet{
		nodeSet("h100", true),
		nodeSet("h200", true),
		nodeSet("cpu", false),
	}

	t.Run("empty refs select everything", func(t *testing.T) {
		assert.Equal(t, all, selectNodeSets(all, nil))
	})

	t.Run("ALL selects everything", func(t *testing.T) {
		assert.Equal(t, all, selectNodeSets(all, []string{consts.SlurmTopologyNodeSetRefAll}))
	})

	t.Run("named refs select a subset", func(t *testing.T) {
		selected := selectNodeSets(all, []string{"h100", "h200"})
		require.Len(t, selected, 2)
		assert.Equal(t, "h100", selected[0].Name)
		assert.Equal(t, "h200", selected[1].Name)
	})

	t.Run("unknown refs are dropped, not fatal", func(t *testing.T) {
		selected := selectNodeSets(all, []string{"h100", "not-applied-yet"})
		require.Len(t, selected, 1)
		assert.Equal(t, "h100", selected[0].Name)
	})
}

func TestScopeGPUPodsToNodes(t *testing.T) {
	gpuPodsByNode := map[string][]string{
		"k8s-node-a": {"h100-0", "cpu-0"},
		"k8s-node-b": {"cpu-1"},
	}

	scoped := scopeGPUPodsToNodes(gpuPodsByNode, []string{"h100-0", "h100-1"})

	assert.Equal(t, map[string][]string{"k8s-node-a": {"h100-0"}}, scoped,
		"workers of NodeSets outside the topology must not land on its switches")
}

func TestMarkClusterDefault(t *testing.T) {
	t.Run("promotes the first entry when the cluster says nothing", func(t *testing.T) {
		entries := []topologyYAMLEntry{{Topology: "a"}, {Topology: "b"}}
		markClusterDefault(context.Background(), entries)
		assert.True(t, entries[0].ClusterDefault)
		assert.False(t, entries[1].ClusterDefault)
	})

	t.Run("promotes even when every topology asked not to be the default", func(t *testing.T) {
		// Slurm uses index 0 unconditionally, so opting out cannot leave the cluster without one.
		entries := []topologyYAMLEntry{{Topology: "a"}, {Topology: "b"}}
		markClusterDefault(context.Background(), entries)
		assert.True(t, entries[0].ClusterDefault)
		assert.False(t, entries[1].ClusterDefault)
	})

	t.Run("keeps the first flagged entry and clears the rest", func(t *testing.T) {
		entries := []topologyYAMLEntry{
			{Topology: "a"},
			{Topology: "b", ClusterDefault: true},
			{Topology: "c", ClusterDefault: true},
		}
		markClusterDefault(context.Background(), entries)
		assert.False(t, entries[0].ClusterDefault)
		assert.True(t, entries[1].ClusterDefault)
		assert.False(t, entries[2].ClusterDefault)
	})

	t.Run("tolerates an empty list", func(t *testing.T) {
		assert.NotPanics(t, func() { markClusterDefault(context.Background(), nil) })
	})
}

func TestBuildTopologyEntry(t *testing.T) {
	ctx := context.Background()
	labelsByNode := map[string]NodeTopologyLabels{
		"k8s-node-a": {"tier-0": "block1", "tier-1": "leaf1"},
	}
	gpuPodsByNode := map[string][]string{"k8s-node-a": {"h100-0"}}
	nodeSets := []v1alpha1.NodeSet{nodeSet("h100", true)}

	t.Run("block carries block_sizes and blocks", func(t *testing.T) {
		entry, err := buildTopologyEntry(ctx, slurmv1.NamedTopology{
			Name: "topo-block",
			Topo: slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeBlock, BlockSizes: []int{4, 16}},
		}, labelsByNode, gpuPodsByNode, nodeSets)
		require.NoError(t, err)

		require.NotNil(t, entry.Block)
		assert.Nil(t, entry.Tree)
		assert.Equal(t, []int{4, 16}, entry.Block.BlockSizes)
		assert.Equal(t, []blockYAML{
			{Block: "block1", Nodes: "h100-0"},
			{Block: "unknown", Nodes: "h100-1"},
		}, entry.Block.Blocks)
	})

	t.Run("tree carries switches", func(t *testing.T) {
		entry, err := buildTopologyEntry(ctx, slurmv1.NamedTopology{
			Name: "topo-tree",
			Topo: slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
		}, labelsByNode, gpuPodsByNode, nodeSets)
		require.NoError(t, err)

		require.NotNil(t, entry.Tree)
		assert.Nil(t, entry.Block)
		assert.NotEmpty(t, entry.Tree.Switches)
	})

	t.Run("unsupported type is rejected", func(t *testing.T) {
		_, err := buildTopologyEntry(ctx, slurmv1.NamedTopology{
			Name: "topo-x",
			Topo: slurmv1.TopologyPluginSpec{Type: "torus3d"},
		}, labelsByNode, gpuPodsByNode, nodeSets)
		assert.ErrorContains(t, err, `unsupported topology type "torus3d"`)
	})
}

func TestOtherTopologyConfigKey(t *testing.T) {
	assert.Equal(t, consts.ConfigMapKeyTopologyConfig, otherTopologyConfigKey(consts.ConfigMapKeyTopologyYAML))
	assert.Equal(t, consts.ConfigMapKeyTopologyYAML, otherTopologyConfigKey(consts.ConfigMapKeyTopologyConfig))
}

func TestCheckMultiTopologyConfigured(t *testing.T) {
	withTopologies := clusterWithTopologies(slurmv1.NamedTopology{
		Name: "topo1",
		Topo: slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
	})

	t.Run("rejects an enabled cluster that defines no topology", func(t *testing.T) {
		r := &WorkerTopologyReconciler{multiTopologyEnabled: true}

		err := r.checkMultiTopologyConfigured(&slurmv1.SlurmCluster{})

		require.Error(t, err)
		assert.ErrorContains(t, err, consts.EnvEnableMultiTopology)
		assert.ErrorContains(t, err, "spec.topology.topologies is empty")
	})

	t.Run("accepts an enabled cluster that defines topologies", func(t *testing.T) {
		r := &WorkerTopologyReconciler{multiTopologyEnabled: true}
		assert.NoError(t, r.checkMultiTopologyConfigured(withTopologies))
	})

	t.Run("with the flag off an empty topology section is fine", func(t *testing.T) {
		r := &WorkerTopologyReconciler{multiTopologyEnabled: false}
		assert.NoError(t, r.checkMultiTopologyConfigured(&slurmv1.SlurmCluster{}))
	})
}

func TestIsClusterReconciliationNeeded(t *testing.T) {
	r := &WorkerTopologyReconciler{multiTopologyEnabled: true}

	t.Run("a cluster wanting no topology at all is left alone", func(t *testing.T) {
		cluster := &slurmv1.SlurmCluster{}
		cluster.Spec.SlurmConfig.TopologyPlugin = ""

		assert.False(t, r.isClusterReconciliationNeeded(cluster),
			"the flag must not drag topology-less clusters into reconciliation")
	})

	t.Run("a legacy plugin still asks for topology", func(t *testing.T) {
		cluster := &slurmv1.SlurmCluster{}
		cluster.Spec.SlurmConfig.TopologyPlugin = consts.SlurmTopologyTree

		assert.True(t, r.isClusterReconciliationNeeded(cluster))
	})

	t.Run("named topologies ask for topology regardless of the plugin", func(t *testing.T) {
		cluster := clusterWithTopologies(slurmv1.NamedTopology{
			Name: "topo1",
			Topo: slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
		})

		assert.True(t, r.isClusterReconciliationNeeded(cluster))
	})
}
