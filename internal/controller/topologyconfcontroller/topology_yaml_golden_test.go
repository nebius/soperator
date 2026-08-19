package topologyconfcontroller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

// nodeLabelsCM builds the topology-node-labels ConfigMap the way NodeTopologyReconciler writes it:
// one JSON-encoded label set per Kubernetes node.
func nodeLabelsCM(labels map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{Data: labels}
}

func clusterWithTopologies(topologies ...slurmv1.NamedTopology) *slurmv1.SlurmCluster {
	cluster := &slurmv1.SlurmCluster{}
	cluster.Spec.Topology = &slurmv1.Topology{Topologies: topologies}
	return cluster
}

func gpuNodeSet(name string, replicas int32, fabric string) v1alpha1.NodeSet {
	ns := v1alpha1.NodeSet{}
	ns.Name = name
	ns.Spec.Replicas = replicas
	ns.Spec.GPU.Enabled = true
	ns.Spec.Topology.Fabric = fabric
	return ns
}

func cpuNodeSet(name string, replicas int32) v1alpha1.NodeSet {
	ns := v1alpha1.NodeSet{}
	ns.Name = name
	ns.Spec.Replicas = replicas
	return ns
}

// TestBuildMultiTopologyYAML_Golden pins the exact topology.yaml a SlurmCluster spec renders to.
func TestBuildMultiTopologyYAML_Golden(t *testing.T) {
	ctx := context.Background()
	r := &WorkerTopologyReconciler{multiTopologyEnabled: true}

	tests := []struct {
		name       string
		cluster    *slurmv1.SlurmCluster
		nodeSets   []v1alpha1.NodeSet
		nodeLabels map[string]string
		gpuPods    map[string][]string
		expected   string
	}{
		{
			name: "single tree topology over every NodeSet",
			cluster: clusterWithTopologies(slurmv1.NamedTopology{
				Name:           "default",
				ClusterDefault: ptr.To(true),
				Topo:           slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
				NodeSetRefs:    []string{consts.SlurmTopologyNodeSetRefAll},
			}),
			nodeSets: []v1alpha1.NodeSet{gpuNodeSet("h100", 4, "")},
			nodeLabels: map[string]string{
				"k8s-a": `{"tier-1":"leaf1","tier-2":"spine1"}`,
			},
			gpuPods: map[string][]string{"k8s-a": {"h100-0", "h100-1"}},
			expected: `- topology: default
  cluster_default: true
  tree:
    switches:
        - switch: leaf1
          nodes: h100-[0-1]
        - switch: root
          children: spine1,unknown
        - switch: spine1
          children: leaf1
        - switch: unknown
          nodes: h100-[2-3]
`,
		},
		{
			name: "heterogeneous fabrics: block for GPU, generated flat for CPU",
			cluster: clusterWithTopologies(
				slurmv1.NamedTopology{
					Name:           "ib-gpu",
					ClusterDefault: ptr.To(true),
					Topo: slurmv1.TopologyPluginSpec{
						Type:       consts.SlurmTopologyTypeBlock,
						BlockSizes: []int{4, 16},
					},
					NodeSetRefs: []string{"h100"},
				},
				// Declared over the CPU NodeSet on purpose: CPU-only nodes are pulled out of
				// user topologies, so this one reaches nothing and the generated flat topology
				// takes its place.
				slurmv1.NamedTopology{
					Name:        "eth-cpu",
					Topo:        slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
					NodeSetRefs: []string{"cpu"},
				},
			),
			nodeSets: []v1alpha1.NodeSet{gpuNodeSet("h100", 4, ""), cpuNodeSet("cpu", 2)},
			nodeLabels: map[string]string{
				"k8s-a": `{"tier-0":"block7","tier-1":"leaf3"}`,
			},
			gpuPods: map[string][]string{"k8s-a": {"h100-0", "h100-1", "h100-2", "h100-3"}},
			expected: `- topology: ib-gpu
  cluster_default: true
  block:
    block_sizes:
        - 4
        - 16
    blocks:
        - block: block7
          nodes: h100-[0-3]
- topology: cpu
  cluster_default: false
  flat: true
`,
		},
		{
			name: "overlapping topologies describe the same NodeSet twice",
			cluster: clusterWithTopologies(
				slurmv1.NamedTopology{
					Name:        "as-blocks",
					Topo:        slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeBlock},
					NodeSetRefs: []string{"h100"},
				},
				slurmv1.NamedTopology{
					Name:           "as-tree",
					ClusterDefault: ptr.To(true),
					Topo:           slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
					NodeSetRefs:    []string{"h100"},
				},
			),
			nodeSets:   []v1alpha1.NodeSet{gpuNodeSet("h100", 2, "")},
			nodeLabels: map[string]string{"k8s-a": `{"tier-0":"block1","tier-1":"leaf1"}`},
			gpuPods:    map[string][]string{"k8s-a": {"h100-0", "h100-1"}},
			expected: `- topology: as-blocks
  cluster_default: false
  block:
    blocks:
        - block: block1
          nodes: h100-[0-1]
- topology: as-tree
  cluster_default: true
  tree:
    switches:
        - switch: leaf1
          nodes: h100-[0-1]
        - switch: root
          children: leaf1
`,
		},
		{
			name: "no clusterDefault set promotes the first topology",
			cluster: clusterWithTopologies(
				slurmv1.NamedTopology{
					Name:        "first",
					Topo:        slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeBlock},
					NodeSetRefs: []string{"h100"},
				},
				slurmv1.NamedTopology{
					Name:        "second",
					Topo:        slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeBlock},
					NodeSetRefs: []string{"h200"},
				},
			),
			nodeSets: []v1alpha1.NodeSet{gpuNodeSet("h100", 1, ""), gpuNodeSet("h200", 1, "")},
			expected: `- topology: first
  cluster_default: true
  block:
    blocks:
        - block: unknown
          nodes: h100-0
- topology: second
  cluster_default: false
  block:
    blocks:
        - block: unknown
          nodes: h200-0
`,
		},
		{
			name: "several clusterDefault flags collapse to the first",
			cluster: clusterWithTopologies(
				slurmv1.NamedTopology{
					Name:           "first",
					ClusterDefault: ptr.To(false),
					Topo:           slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeBlock},
					NodeSetRefs:    []string{"h100"},
				},
				slurmv1.NamedTopology{
					Name:           "second",
					ClusterDefault: ptr.To(true),
					Topo:           slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeBlock},
					NodeSetRefs:    []string{"h200"},
				},
				slurmv1.NamedTopology{
					Name:           "third",
					ClusterDefault: ptr.To(true),
					Topo:           slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeBlock},
					NodeSetRefs:    []string{"h300"},
				},
			),
			nodeSets: []v1alpha1.NodeSet{
				gpuNodeSet("h100", 1, ""), gpuNodeSet("h200", 1, ""), gpuNodeSet("h300", 1, ""),
			},
			expected: `- topology: first
  cluster_default: false
  block:
    blocks:
        - block: unknown
          nodes: h100-0
- topology: second
  cluster_default: true
  block:
    blocks:
        - block: unknown
          nodes: h200-0
- topology: third
  cluster_default: false
  block:
    blocks:
        - block: unknown
          nodes: h300-0
`,
		},
		{
			name: "a topology matching no NodeSet is left out",
			cluster: clusterWithTopologies(
				slurmv1.NamedTopology{
					Name:           "present",
					ClusterDefault: ptr.To(true),
					Topo:           slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeBlock},
					NodeSetRefs:    []string{"h100"},
				},
				slurmv1.NamedTopology{
					Name:        "not-applied-yet",
					Topo:        slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeBlock},
					NodeSetRefs: []string{"missing"},
				},
			),
			nodeSets: []v1alpha1.NodeSet{gpuNodeSet("h100", 1, "")},
			expected: `- topology: present
  cluster_default: true
  block:
    blocks:
        - block: unknown
          nodes: h100-0
`,
		},
		{
			name: "CPU-only NodeSets leave the ALL topology for the generated flat one",
			cluster: clusterWithTopologies(slurmv1.NamedTopology{
				Name:           "ib",
				ClusterDefault: ptr.To(true),
				Topo:           slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
				NodeSetRefs:    []string{consts.SlurmTopologyNodeSetRefAll},
			}),
			nodeSets:   []v1alpha1.NodeSet{gpuNodeSet("h100", 4, ""), cpuNodeSet("cpu", 8)},
			nodeLabels: map[string]string{"k8s-a": `{"tier-1":"leaf01"}`},
			gpuPods:    map[string][]string{"k8s-a": {"h100-0", "h100-1", "h100-2", "h100-3"}},
			expected: `- topology: ib
  cluster_default: true
  tree:
    switches:
        - switch: leaf01
          nodes: h100-[0-3]
        - switch: root
          children: leaf01
- topology: cpu
  cluster_default: false
  flat: true
`,
		},
		{
			name: "a CPU-only cluster is left with the flat topology as its default",
			cluster: clusterWithTopologies(slurmv1.NamedTopology{
				Name:        "ib",
				Topo:        slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
				NodeSetRefs: []string{consts.SlurmTopologyNodeSetRefAll},
			}),
			nodeSets: []v1alpha1.NodeSet{cpuNodeSet("cpu", 4)},
			expected: `- topology: cpu
  cluster_default: true
  flat: true
`,
		},
		{
			name: "a configured topology named cpu wins over the generated one",
			cluster: clusterWithTopologies(slurmv1.NamedTopology{
				Name:           consts.SlurmTopologyCPUOnlyName,
				ClusterDefault: ptr.To(true),
				Topo:           slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
				NodeSetRefs:    []string{"h100"},
			}),
			nodeSets: []v1alpha1.NodeSet{gpuNodeSet("h100", 2, ""), cpuNodeSet("worker", 2)},
			expected: `- topology: cpu
  cluster_default: true
  tree:
    switches:
        - switch: root
          children: unknown
        - switch: unknown
          nodes: h100-[0-1]
`,
		},
		{
			name: "named fabrics keep their catch-all units apart",
			cluster: clusterWithTopologies(slurmv1.NamedTopology{
				Name:           "multi-fabric",
				ClusterDefault: ptr.To(true),
				Topo:           slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
			}),
			nodeSets: []v1alpha1.NodeSet{
				gpuNodeSet("a", 1, "fab-a"),
				gpuNodeSet("b", 1, "fab-b"),
			},
			expected: `- topology: multi-fabric
  cluster_default: true
  tree:
    switches:
        - switch: fab-a
          children: fab-a.unknown
        - switch: fab-a.unknown
          nodes: a-0
        - switch: fab-b
          children: fab-b.unknown
        - switch: fab-b.unknown
          nodes: b-0
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := r.buildMultiTopologyYAML(
				ctx, tt.cluster, tt.nodeSets, nodeLabelsCM(tt.nodeLabels), tt.gpuPods,
			)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, out)
		})
	}
}

// TestDefaultTopologyConfig_Golden pins the placeholder written before any topology is discovered.
func TestDefaultTopologyConfig_Golden(t *testing.T) {
	ctx := context.Background()
	r := &WorkerTopologyReconciler{multiTopologyEnabled: true}

	t.Run("with the flag off the legacy bare root switch is kept", func(t *testing.T) {
		legacy := &WorkerTopologyReconciler{multiTopologyEnabled: false}

		out, err := legacy.defaultTopologyConfig(ctx, &slurmv1.SlurmCluster{})
		require.NoError(t, err)
		assert.Equal(t, "SwitchName=root", out)
	})

	t.Run("multi-topology cluster defines every topology without members", func(t *testing.T) {
		cluster := clusterWithTopologies(
			slurmv1.NamedTopology{
				Name: "ib-gpu",
				Topo: slurmv1.TopologyPluginSpec{
					Type:       consts.SlurmTopologyTypeBlock,
					BlockSizes: []int{4},
				},
			},
			slurmv1.NamedTopology{
				Name: "eth-cpu",
				Topo: slurmv1.TopologyPluginSpec{Type: consts.SlurmTopologyTypeTree},
			},
		)

		out, err := r.defaultTopologyConfig(ctx, cluster)
		require.NoError(t, err)
		assert.Equal(t, `- topology: ib-gpu
  cluster_default: true
  block:
    block_sizes:
        - 4
    blocks: []
- topology: eth-cpu
  cluster_default: false
  tree:
    switches:
        - switch: root
`, out)
	})
}
