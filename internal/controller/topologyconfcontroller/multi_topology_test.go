package topologyconfcontroller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"

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
		(&WorkerTopologyReconciler{}).markClusterDefault(context.Background(), &slurmv1.SlurmCluster{}, entries)
		assert.True(t, entries[0].ClusterDefault)
		assert.False(t, entries[1].ClusterDefault)
	})

	t.Run("promotes even when every topology asked not to be the default", func(t *testing.T) {
		// Slurm uses index 0 unconditionally, so opting out cannot leave the cluster without one.
		entries := []topologyYAMLEntry{{Topology: "a"}, {Topology: "b"}}
		(&WorkerTopologyReconciler{}).markClusterDefault(context.Background(), &slurmv1.SlurmCluster{}, entries)
		assert.True(t, entries[0].ClusterDefault)
		assert.False(t, entries[1].ClusterDefault)
	})

	t.Run("keeps the first flagged entry and clears the rest", func(t *testing.T) {
		entries := []topologyYAMLEntry{
			{Topology: "a"},
			{Topology: "b", ClusterDefault: true},
			{Topology: "c", ClusterDefault: true},
		}
		(&WorkerTopologyReconciler{}).markClusterDefault(context.Background(), &slurmv1.SlurmCluster{}, entries)
		assert.False(t, entries[0].ClusterDefault)
		assert.True(t, entries[1].ClusterDefault)
		assert.False(t, entries[2].ClusterDefault)
	})

	t.Run("tolerates an empty list", func(t *testing.T) {
		assert.NotPanics(t, func() {
			(&WorkerTopologyReconciler{}).markClusterDefault(context.Background(), &slurmv1.SlurmCluster{}, nil)
		})
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
			Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeBlock, BlockSizes: []int{4, 16}},
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
			Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeTree},
		}, labelsByNode, gpuPodsByNode, nodeSets)
		require.NoError(t, err)

		require.NotNil(t, entry.Tree)
		assert.Nil(t, entry.Block)
		assert.NotEmpty(t, entry.Tree.Switches)
	})

	t.Run("unsupported type is rejected", func(t *testing.T) {
		_, err := buildTopologyEntry(ctx, slurmv1.NamedTopology{
			Name: "topo-x",
			Topo: slurmv1.TopologyPlugin{Type: "torus3d"},
		}, labelsByNode, gpuPodsByNode, nodeSets)
		assert.ErrorContains(t, err, `unsupported topology type "torus3d"`)
	})
}

func TestIsClusterReconciliationNeeded(t *testing.T) {
	r := &WorkerTopologyReconciler{}

	t.Run("a cluster declaring no topology is left alone", func(t *testing.T) {
		assert.False(t, r.isClusterReconciliationNeeded(&slurmv1.SlurmCluster{}),
			"there is nothing to render, and nothing to fail over")
	})

	t.Run("named topologies ask for topology regardless of the plugin", func(t *testing.T) {
		cluster := clusterWithTopologies(slurmv1.NamedTopology{
			Name: "topo1",
			Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeTree},
		})

		assert.True(t, r.isClusterReconciliationNeeded(cluster))
	})
}

// TestEmptyTopologyEntry pins the shapes Slurm accepts for a topology that reaches no node yet.
// Both plugins fatal on an empty member list -- "No switches configured" and "No blocks configured"
// -- so each carries one named, empty member.
func TestEmptyTopologyEntry(t *testing.T) {
	t.Run("tree carries a single named switch", func(t *testing.T) {
		entry := emptyTopologyEntry(slurmv1.NamedTopology{
			Name: "tree-ib",
			Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeTree},
		})

		require.NotNil(t, entry.Tree)
		require.Len(t, entry.Tree.Switches, 1)
		assert.NotEmpty(t, entry.Tree.Switches[0].Switch)
		assert.Empty(t, entry.Tree.Switches[0].Nodes)
	})

	t.Run("block carries a named block and keeps the configured sizes", func(t *testing.T) {
		entry := emptyTopologyEntry(slurmv1.NamedTopology{
			Name: "block-nvl72",
			Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeBlock, BlockSizes: []int{18}},
		})

		require.NotNil(t, entry.Block)
		assert.Equal(t, []int{18}, entry.Block.BlockSizes)
		require.Len(t, entry.Block.Blocks, 1)
		assert.NotEmpty(t, entry.Block.Blocks[0].Block)
		assert.Empty(t, entry.Block.Blocks[0].Nodes)
	})

	t.Run("block without configured sizes still gets one", func(t *testing.T) {
		// Slurm fatals with "Blocks do not contain any nodes and the BlockSizes are not set" when
		// it can derive a base block size from neither.
		entry := emptyTopologyEntry(slurmv1.NamedTopology{
			Name: "block-nvl72",
			Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeBlock},
		})

		require.NotNil(t, entry.Block)
		assert.NotEmpty(t, entry.Block.BlockSizes)
	})

	t.Run("flat needs no members at all", func(t *testing.T) {
		entry := emptyTopologyEntry(slurmv1.NamedTopology{
			Name: "flat",
			Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeFlat},
		})

		assert.True(t, entry.Flat)
		assert.Nil(t, entry.Tree)
		assert.Nil(t, entry.Block)
	})
}

// drainEvents returns everything the recorder captured.
func drainEvents(recorder *events.FakeRecorder) []string {
	var captured []string
	for {
		select {
		case event := <-recorder.Events:
			captured = append(captured, event)
		default:
			return captured
		}
	}
}

// TestTopologyEvents pins the misconfigurations reported on the SlurmCluster. Each is invisible
// otherwise: the config renders, the operator stays healthy, and the first symptom is a job that
// cannot be scheduled.
func TestTopologyEvents(t *testing.T) {
	ctx := context.Background()

	t.Run("a topology reaching no node is reported", func(t *testing.T) {
		recorder := events.NewFakeRecorder(10)
		r := &WorkerTopologyReconciler{recorder: recorder}

		cluster := clusterWithTopologies(slurmv1.NamedTopology{
			Name:        "tree-ib",
			Topo:        slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeTree},
			NodeSetRefs: []string{"never-applied"},
		})

		_, err := r.buildMultiTopologyYAML(ctx, cluster, nil, nodeLabelsCM(nil), nil)
		require.NoError(t, err)

		captured := drainEvents(recorder)
		require.Len(t, captured, 1)
		assert.Contains(t, captured[0], reasonTopologyReachesNoNode)
		assert.Contains(t, captured[0], "never-applied")
	})

	t.Run("a partition bound to a topology outside the config is reported", func(t *testing.T) {
		recorder := events.NewFakeRecorder(10)
		r := &WorkerTopologyReconciler{recorder: recorder}

		cluster := clusterWithTopologies(slurmv1.NamedTopology{
			Name: "flat",
			Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeFlat},
		})
		cluster.Spec.PartitionConfiguration.Partitions = []slurmv1.Partition{
			{Name: "gpu", TopologyRef: "block-nvl72"},
			{Name: "main", TopologyRef: "flat"},
		}

		_, err := r.buildMultiTopologyYAML(ctx, cluster, nil, nodeLabelsCM(nil), nil)
		require.NoError(t, err)

		captured := drainEvents(recorder)
		require.Len(t, captured, 1, "only the unresolved ref is reported")
		assert.Contains(t, captured[0], reasonUnresolvedTopologyRef)
		assert.Contains(t, captured[0], "block-nvl72")
	})

	t.Run("several topologies requesting clusterDefault are reported", func(t *testing.T) {
		recorder := events.NewFakeRecorder(10)
		r := &WorkerTopologyReconciler{recorder: recorder}

		entries := []topologyYAMLEntry{
			{Topology: "flat", ClusterDefault: true},
			{Topology: "tree-ib", ClusterDefault: true},
		}

		r.markClusterDefault(ctx, &slurmv1.SlurmCluster{}, entries)

		captured := drainEvents(recorder)
		require.Len(t, captured, 1)
		assert.Contains(t, captured[0], reasonClusterDefaultConflict)
		assert.Contains(t, captured[0], "tree-ib")
	})

	t.Run("a healthy config reports nothing", func(t *testing.T) {
		recorder := events.NewFakeRecorder(10)
		r := &WorkerTopologyReconciler{recorder: recorder}

		cluster := clusterWithTopologies(slurmv1.NamedTopology{
			Name:           "flat",
			ClusterDefault: ptr.To(true),
			Topo:           slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeFlat},
		})

		_, err := r.buildMultiTopologyYAML(ctx, cluster, nil, nodeLabelsCM(nil), nil)
		require.NoError(t, err)

		assert.Empty(t, drainEvents(recorder))
	})
}

// TestFlatTopologyReachingNoNodeIsNotReported pins that a flat topology covering nothing stays
// quiet: it lists no nodes by design, so "reaches no node" is its normal state.
func TestFlatTopologyReachingNoNodeIsNotReported(t *testing.T) {
	recorder := events.NewFakeRecorder(10)
	r := &WorkerTopologyReconciler{recorder: recorder}

	cluster := clusterWithTopologies(slurmv1.NamedTopology{
		Name: "flat",
		Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeFlat},
	})

	_, err := r.buildMultiTopologyYAML(context.Background(), cluster, nil, nodeLabelsCM(nil), nil)
	require.NoError(t, err)

	assert.Empty(t, drainEvents(recorder))
}
