package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

func TestTopologySchedulingPrerequisites(t *testing.T) {
	for _, tt := range []struct {
		name        string
		config      string
		treeDefault bool
		laterTree   bool
		wantAllSkip bool
		wantTree    string
	}{
		{
			name: "all partitions use flat",
			config: `{"configType":"structured","partitions":[
				{"name":"main","topologyRef":"flat"},{"name":"hidden","topologyRef":"flat"}]}`,
			wantAllSkip: true,
		},
		{
			name: "every topology has a partition",
			config: `{"configType":"structured","partitions":[
				{"name":"main","topologyRef":"flat"},{"name":"gpu","topologyRef":"tree-ib"}]}`,
			wantTree: "tree-ib",
		},
		{
			name: "omitted reference uses explicit cluster default",
			config: `{"configType":"structured","partitions":[
				{"name":"main"},{"name":"cpu","topologyRef":"flat"}]}`,
			treeDefault: true, wantTree: "tree-ib",
		},
		{
			name: "omitted reference uses promoted first topology",
			config: `{"configType":"structured","partitions":[
				{"name":"main"},{"name":"gpu","topologyRef":"tree-ib"}]}`,
			wantTree: "tree-ib",
		},
		{
			name: "generated partitions use cluster default", config: `{"configType":"default"}`,
			treeDefault: true, wantAllSkip: true, wantTree: "tree-ib",
		},
		{
			name: "raw partitions use explicit and default bindings",
			config: `{"configType":"custom","rawConfig":[
				"PartitionName=main Nodes=ALL", "PartitionName=gpu Nodes=ALL Topology=tree-ib"]}`,
			wantTree: "tree-ib",
		},
		{
			name:        "raw partitions all use flat",
			config:      `{"configType":"custom","rawConfig":["PartitionName=main Nodes=ALL Topology=flat"]}`,
			wantAllSkip: true,
		},
		{
			name:        "dangling structured reference falls back to cluster default",
			config:      `{"configType":"structured","partitions":[{"name":"main","topologyRef":"missing"}]}`,
			wantAllSkip: true,
		},
		{
			name:      "unbound first tree does not hide another bound tree",
			config:    `{"configType":"structured","partitions":[{"name":"gpu","topologyRef":"tree-other"}]}`,
			laterTree: true, wantAllSkip: true, wantTree: "tree-other",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cluster := kubeobjects.SlurmCluster{Spec: kubeobjects.SlurmClusterSpec{
				Topology: &kubeobjects.SlurmClusterTopology{Topologies: []kubeobjects.NamedTopology{
					{Name: "flat", Topo: kubeobjects.TopologyPlugin{Type: topologyKindFlat}},
					{Name: "tree-ib", ClusterDefault: &tt.treeDefault, Topo: kubeobjects.TopologyPlugin{Type: topologyKindTree}},
				}},
			}}
			require.NoError(t, json.Unmarshal([]byte(tt.config), &cluster.Spec.PartitionConfiguration))
			s := newTopologySchedulingTest(t, cluster)
			if tt.laterTree {
				cluster.Spec.Topology.Topologies = append(cluster.Spec.Topology.Topologies,
					kubeobjects.NamedTopology{Name: "tree-other", Topo: kubeobjects.TopologyPlugin{Type: topologyKindTree}})
				s = newTopologySchedulingTest(t, cluster)
				other := s.rendered[1]
				other.Name = "tree-other"
				s.rendered = append(s.rendered, other)
			}
			err := s.everyTopologyIsUsedByPartition(t.Context())
			if tt.wantAllSkip {
				require.ErrorIs(t, err, godog.ErrSkip)
			} else {
				require.NoError(t, err)
			}
			err = s.clusterIsConfiguredWithTreeTopology(t.Context())
			if tt.wantTree == "" {
				require.ErrorIs(t, err, godog.ErrSkip)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantTree, s.treeTopology)
			}
		})
	}
}

func newTopologySchedulingTest(t *testing.T, cluster kubeobjects.SlurmCluster) *Topology {
	t.Helper()
	data, err := json.Marshal(cluster)
	require.NoError(t, err)
	runtime := blockRankTestRuntime{kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		assert.Equal(t, []string{"get", "slurmcluster", "cluster", "-n", framework.SoperatorNamespace, "-o", "json"}, args)
		return string(data), nil
	})}
	rendered, err := parseTopologyEntries(renderedTopologyConfig)
	require.NoError(t, err)
	return &Topology{
		info: &framework.ClusterInfo{SlurmClusterName: "cluster"}, runtime: runtime,
		kubectl: framework.NewKubectlClient(runtime), rendered: rendered[:2],
	}
}

func TestTopologySchedulingKeepsRuntimeFailures(t *testing.T) {
	cluster := kubeobjects.SlurmCluster{Spec: kubeobjects.SlurmClusterSpec{
		Topology: &kubeobjects.SlurmClusterTopology{Topologies: []kubeobjects.NamedTopology{
			{Name: "flat"}, {Name: "tree-ib", Topo: kubeobjects.TopologyPlugin{Type: topologyKindTree}},
		}},
		PartitionConfiguration: kubeobjects.PartitionConfiguration{ConfigType: "custom", RawConfig: []string{
			"PartitionName=main Topology=flat", "PartitionName=gpu Topology=tree-ib",
		}},
	}}
	s := newTopologySchedulingTest(t, cluster)
	require.NoError(t, s.everyTopologyIsUsedByPartition(t.Context()))
	require.NoError(t, s.clusterIsConfiguredWithTreeTopology(t.Context()))
	s.partitions = []partitionInfo{{Name: "main", Topology: "flat", Nodes: "worker-0"}}
	s.nodes = map[string]slurmNodeView{"worker-0": {Info: framework.SlurmNodeInfo{State: "IDLE"}}}
	assert.ErrorContains(t, s.runJobInTopology(t.Context(), "tree-ib"), "bound to no partition")
	assert.ErrorContains(t, s.pickWorkersOnDifferentLeaves(t.Context()), "bound to no partition")
	rendered := s.rendered
	s.rendered = s.rendered[:1]
	assert.ErrorContains(t, s.clusterIsConfiguredWithTreeTopology(t.Context()), "missing from the published config")
	s.rendered = rendered

	s.partitions[0].Topology = "tree-ib"
	s.nodes["worker-0"] = slurmNodeView{Info: framework.SlurmNodeInfo{State: "DOWN"}}
	assert.ErrorContains(t, s.runJobInTopology(t.Context(), "tree-ib"), "no worker")
	assert.Contains(t, s.topologyWorkerReport(t.Context(), "tree-ib"), "state=DOWN")

	runtime := blockRankTestRuntime{kubectl: framework.NewArgsScope(func(context.Context, ...string) (string, error) {
		return "invalid cluster JSON", nil
	})}
	s.runtime, s.kubectl = runtime, framework.NewKubectlClient(runtime)
	assert.ErrorContains(t, s.everyTopologyIsUsedByPartition(t.Context()), "decode kubectl")
	assert.ErrorContains(t, s.clusterIsConfiguredWithTreeTopology(t.Context()), "decode kubectl")
}

func TestTopologySchedulingResolvesImplicitDefaultPartition(t *testing.T) {
	s := newTopologySchedulingTest(t, kubeobjects.SlurmCluster{})
	s.partitions = []partitionInfo{{Name: "main", Nodes: "worker-0"}}
	s.nodes = map[string]slurmNodeView{"worker-0": {Info: framework.SlurmNodeInfo{State: "IDLE"}}}
	partition, ok := s.partitionOfTopology("flat")
	require.True(t, ok)
	assert.Equal(t, "main", partition)
	partition, node, ok, err := s.pickReadyNodeOfTopology(t.Context(), "flat")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "main", partition)
	assert.Equal(t, "worker-0", node)
	assert.Contains(t, s.topologyWorkerReport(t.Context(), "flat"), "worker-0 of partition main")
	runtime := s.runtime.(blockRankTestRuntime)
	jobs := 0
	runtime.jail = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		jobs++
		assert.Contains(t, command, "-p 'main'")
		assert.Contains(t, command, "-w 'worker-0'")
		return "worker-0\n", nil
	})
	s.runtime = runtime
	require.NoError(t, s.runJobInTopology(t.Context(), "flat"))
	assert.Equal(t, 1, jobs)
}

// renderedTopologyConfig is the topology.yaml the operator publishes, warning header included.
const renderedTopologyConfig = `# Managed by Soperator.
# Do not edit this file.
# Changes are overwritten.

- topology: flat
  cluster_default: true
  flat: true
- topology: tree-ib
  cluster_default: false
  tree:
    switches:
        - switch: leaf-a
          nodes: worker-1
        - switch: spine-a
          children: leaf-a,leaf-b
        - switch: leaf-b
          nodes: worker-block-0
        - switch: fabric
          children: spine-a,fabric.unknown
        - switch: fabric.unknown
          nodes: worker-2
- topology: block-nvl72
  cluster_default: false
  block:
    block_sizes:
        - 2
    blocks:
        - block: block-0
          nodes: worker-block-[0-1]
`

// loadedTopologyConfig is the same file as "scontrol show topoconf" prints it back: YAML document
// markers, and the plugins an entry does not use spelled as empty maps.
const loadedTopologyConfig = `%YAML 1.1
---
- topology: flat
  cluster_default: true
  block: {}
  flat: true
  ring: {}
  torus3d: {}
  tree: {}
- topology: tree-ib
  cluster_default: false
  block: {}
  flat: {}
  ring: {}
  torus3d: {}
  tree:
    switches:
    - switch: leaf-a
      children:
      nodes: worker-1
    - switch: spine-a
      children: leaf-a,leaf-b
      nodes:
    - switch: leaf-b
      children:
      nodes: worker-block-0
    - switch: fabric
      children: spine-a,fabric.unknown
      nodes:
    - switch: fabric.unknown
      children:
      nodes: worker-2
- topology: block-nvl72
  cluster_default: false
  block:
    block_sizes:
    - 2
    blocks:
    - block: block-0
      nodes: worker-block-[0-1]
  flat: {}
  ring: {}
  torus3d: {}
  tree: {}
...
`

const showTopologyTree = `SwitchName=leaf-a Level=0 LinkSpeed=0 Nodes=worker-1
SwitchName=spine-a Level=1 LinkSpeed=0 Nodes=worker-1,worker-block-0 Switches=leaf-a,leaf-b
SwitchName=leaf-b Level=0 LinkSpeed=0 Nodes=worker-block-0
SwitchName=fabric Level=2 LinkSpeed=0 Nodes=worker-[0-2],worker-block-[0-1] Switches=spine-a,fabric.unknown
SwitchName=fabric.unknown Level=0 LinkSpeed=0 Nodes=worker-2
`

const showTopologyBlock = `BlockName=block-0 BlockIndex=0 Nodes=worker-block-[0-1] BlockSize=2
`

const showNodes = `NodeName=worker-0 Arch=x86_64 CoresPerSocket=40
   State=IDLE+CLOUD ThreadsPerCore=2 TmpDisk=0 Weight=1 Owner=N/A MCS_label=N/A
   Topology=tree-ib:fabric:spine-a:leaf-a

NodeName=worker-2 CoresPerSocket=40
   State=IDLE+CLOUD+POWERED_DOWN ThreadsPerCore=2 TmpDisk=0 Weight=1 Owner=N/A MCS_label=N/A

NodeName=worker-block-0 Arch=x86_64 CoresPerSocket=40
   State=IDLE+CLOUD ThreadsPerCore=2 TmpDisk=0 Weight=1 Owner=N/A MCS_label=N/A
   Topology=tree-ib:fabric:spine-a:leaf-b,block-nvl72:block-0

NodeName=worker-ephemral-0 Arch=x86_64 CoresPerSocket=8
   State=IDLE+CLOUD ThreadsPerCore=2 TmpDisk=0 Weight=1 Owner=N/A MCS_label=N/A
`

const showPartitions = `PartitionName=worker
   AllocNodes=ALL Default=NO QoS=N/A
   Nodes=worker-[0-2]
   Topology=tree-ib
   State=UP TotalCPUs=480 TotalNodes=3 SelectTypeParameters=NONE

PartitionName=main
   AllocNodes=ALL Default=YES QoS=N/A
   Nodes=worker-[0-2],worker-block-[0-1],worker-ephemral-[0-15]
   Topology=flat
   State=UP TotalCPUs=1056 TotalNodes=21 SelectTypeParameters=NONE
`

func TestParseTopologyEntriesReadsTheRenderedConfig(t *testing.T) {
	entries, err := parseTopologyEntries(renderedTopologyConfig)
	require.NoError(t, err)
	require.Len(t, entries, 3)

	assert.Equal(t, "flat", entries[0].Name)
	assert.Equal(t, topologyKindFlat, entries[0].Kind)
	assert.True(t, entries[0].ClusterDefault)

	assert.Equal(t, topologyKindTree, entries[1].Kind)
	assert.False(t, entries[1].ClusterDefault)
	require.Len(t, entries[1].Switches, 5)
	assert.Equal(t, "leaf-a", entries[1].Switches[0].Name)
	assert.Equal(t, "worker-1", entries[1].Switches[0].Nodes)
	assert.Equal(t, []string{"leaf-a", "leaf-b"}, entries[1].Switches[1].Children)

	assert.Equal(t, topologyKindBlock, entries[2].Kind)
	assert.Equal(t, []int{2}, entries[2].BlockSizes)
	require.Len(t, entries[2].Blocks, 1)
	assert.Equal(t, "worker-block-[0-1]", entries[2].Blocks[0].Nodes)
}

// The two spellings of one file must reduce to the same structure, or comparing what slurmctld
// loaded against what the operator published would report a difference that is only formatting.
func TestParseTopologyEntriesReadsTopoconfTheSameWay(t *testing.T) {
	rendered, err := parseTopologyEntries(renderedTopologyConfig)
	require.NoError(t, err)
	loaded, err := parseTopologyEntries(loadedTopologyConfig)
	require.NoError(t, err)

	assert.Equal(t,
		"block-nvl72=block:[2]:false,flat=flat:[]:true,tree-ib=tree:[]:false",
		topologyStructure(rendered))
	assert.Equal(t, topologyStructure(rendered), topologyStructure(loaded))
}

// slurmctld prints the cluster default first in topoconf whatever the file order was, so a GPU
// cluster whose default is tree-ib lists it before flat while the operator rendered flat first.
func TestTopologyStructureIgnoresEntryOrder(t *testing.T) {
	rendered := []topologyEntry{
		{Name: "flat", Kind: topologyKindFlat},
		{Name: "tree-ib", Kind: topologyKindTree, ClusterDefault: true},
	}
	loaded := []topologyEntry{
		{Name: "tree-ib", Kind: topologyKindTree, ClusterDefault: true},
		{Name: "flat", Kind: topologyKindFlat},
	}
	assert.Equal(t, topologyStructure(rendered), topologyStructure(loaded))
	assert.Equal(t, "flat", rendered[0].Name, "the caller's slice keeps its order")

	loaded[1].ClusterDefault = true
	assert.NotEqual(t, topologyStructure(rendered), topologyStructure(loaded), "a settings difference is still reported")
}

func TestValidateNoWorkersUnderUnknown(t *testing.T) {
	for _, tt := range []struct {
		name    string
		unit    string
		nodes   string
		wantErr bool
	}{
		{name: "bare unknown", unit: "unknown", nodes: "worker-0", wantErr: true},
		{name: "fabric unknown hostlist", unit: "fabric.unknown", nodes: "worker-[0-3]", wantErr: true},
		{name: "empty unknown", unit: "unknown"},
		{name: "null unknown", unit: "fabric.unknown", nodes: "(null)"},
		{name: "whitespace unknown", unit: "unknown", nodes: "  "},
		{name: "known unit", unit: "leaf-a", nodes: "worker-0"},
		{name: "unknown substring", unit: "unknown-leaf", nodes: "worker-0"},
	} {
		for _, kind := range []string{topologyKindTree, topologyKindBlock} {
			t.Run(tt.name+"/"+kind, func(t *testing.T) {
				entry := topologyEntry{Name: "fabric", Kind: kind}
				if kind == topologyKindTree {
					entry.Switches = []topologySwitch{{Name: tt.unit, Nodes: tt.nodes}}
				} else {
					entry.Blocks = []topologyUnit{{Name: tt.unit, Nodes: tt.nodes}}
				}
				err := validateNoWorkersUnderUnknown([]topologyEntry{entry})
				if tt.wantErr {
					require.ErrorContains(t, err, "fabric/"+tt.unit+": "+tt.nodes)
				} else {
					require.NoError(t, err)
				}
			})
		}
	}

	t.Run("loaded topoconf", func(t *testing.T) {
		entries, err := parseTopologyEntries(loadedTopologyConfig)
		require.NoError(t, err)
		require.ErrorContains(t, validateNoWorkersUnderUnknown(entries), "tree-ib/fabric.unknown: worker-2")
	})
}

func TestParseTopologyEntriesRejectsMalformedConfigs(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":         "",
		"no name":       "- cluster_default: true\n  flat: true\n",
		"no plugin":     "- topology: nowhere\n  cluster_default: true\n",
		"two plugins":   "- topology: both\n  flat: true\n  tree:\n    switches:\n      - switch: s\n",
		"duplicate":     "- topology: twice\n  flat: true\n- topology: twice\n  flat: true\n",
		"empty plugins": "- topology: none\n  tree: {}\n  block: {}\n  flat: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTopologyEntries(raw)
			assert.Error(t, err)
		})
	}
}

func TestParseSlurmSwitchesFindsTheLeaves(t *testing.T) {
	switches, err := parseSlurmSwitches(showTopologyTree)
	require.NoError(t, err)
	require.Len(t, switches, 5)

	leaves := make(map[string]string)
	for _, sw := range switches {
		if sw.IsLeaf() {
			leaves[sw.Name] = sw.Nodes
		}
	}
	assert.Equal(t, map[string]string{
		"leaf-a":         "worker-1",
		"leaf-b":         "worker-block-0",
		"fabric.unknown": "worker-2",
	}, leaves)

	assert.Equal(t, 1, switches[1].Level)
	assert.Equal(t, []string{"leaf-a", "leaf-b"}, switches[1].Children)
	// An ancestor lists every descendant node, which is why only Level=0 identifies a node's switch.
	assert.Equal(t, "worker-1,worker-block-0", switches[1].Nodes)
}

func TestParseSlurmSwitchesRejectsOutputWithoutSwitches(t *testing.T) {
	_, err := parseSlurmSwitches("slurm_load_topo error: Requested topology configuration is not available")
	assert.Error(t, err)
}

func TestParseSlurmBlocks(t *testing.T) {
	blocks, err := parseSlurmBlocks(showTopologyBlock)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	assert.Equal(t, slurmBlock{Name: "block-0", Nodes: "worker-block-[0-1]", Size: 2}, blocks[0])
}

func TestParseNodeTopologies(t *testing.T) {
	blocks := parseScontrolBlocks(showNodes, "NodeName")
	require.Len(t, blocks, 4)

	assert.Equal(t,
		map[string]string{"tree-ib": "fabric:spine-a:leaf-a"},
		parseNodeTopologies(blocks["worker-0"]))
	assert.Equal(t,
		map[string]string{"tree-ib": "fabric:spine-a:leaf-b", "block-nvl72": "block-0"},
		parseNodeTopologies(blocks["worker-block-0"]))

	// A powered-down worker and a CPU-only one both register into nothing.
	assert.Nil(t, parseNodeTopologies(blocks["worker-2"]))
	assert.Nil(t, parseNodeTopologies(blocks["worker-ephemral-0"]))
}

func TestRegisteredLeafTakesTheLastPathSegment(t *testing.T) {
	assert.Equal(t, "leaf-a", registeredLeaf("fabric:spine-a:leaf-a"))
	assert.Equal(t, "block-0", registeredLeaf("block-0"))
	assert.Equal(t, "", registeredLeaf(""))
}

func TestParsePartitions(t *testing.T) {
	partitions := parsePartitions(showPartitions)
	assert.Equal(t, []partitionInfo{
		{Name: "main", Topology: "flat", Nodes: "worker-[0-2],worker-block-[0-1],worker-ephemral-[0-15]"},
		{Name: "worker", Topology: "tree-ib", Nodes: "worker-[0-2]"},
	}, partitions)
}

func TestSrunRefusedOnlyMatchesPlacementFailures(t *testing.T) {
	refusal := errors.New(`srun: error: Unable to allocate resources: Requested nodes are busy`)
	assert.True(t, srunRefused("srun: job 108 queued and waiting for resources", refusal))
	assert.True(t, srunRefused("", errors.New("Requested node configuration is not available")))

	assert.False(t, srunRefused("worker-0", nil))
	assert.False(t, srunRefused("", errors.New("error: connection refused")))
}

func TestHalveBlockSizes(t *testing.T) {
	// Only the base is halved: the aggregation levels above it stay as the cluster declares them.
	sizes, ok := halveBlockSizes([]int{4, 16})
	assert.True(t, ok)
	assert.Equal(t, []int{2, 16}, sizes)

	sizes, ok = halveBlockSizes([]int{8})
	assert.True(t, ok)
	assert.Equal(t, []int{4}, sizes)

	// A base size of one cannot be halved into a valid configuration.
	_, ok = halveBlockSizes([]int{1})
	assert.False(t, ok)
	_, ok = halveBlockSizes(nil)
	assert.False(t, ok)
}

func TestPlainHostlistDefersRangesToScontrol(t *testing.T) {
	nodes, ok := plainHostlist("worker-0,worker-1")
	assert.True(t, ok)
	assert.Equal(t, []string{"worker-0", "worker-1"}, nodes)

	nodes, ok = plainHostlist("(null)")
	assert.True(t, ok)
	assert.Nil(t, nodes)

	_, ok = plainHostlist("worker-[0-2]")
	assert.False(t, ok)
}

func TestSrunRequestCommand(t *testing.T) {
	unconstrained := srunRequest{
		Partition: "worker",
		Nodes:     []string{"worker-0", "worker-1"},
		Immediate: topologyJobImmediate,
	}
	assert.Equal(t,
		"timeout 150 srun --immediate=90 -p 'worker' -N 2 -n 2 --ntasks-per-node=1 -w 'worker-0,worker-1' --time=1:00 hostname",
		unconstrained.command())

	constrained := unconstrained
	constrained.Switches = 1
	constrained.Immediate = topologyDenyImmediate
	assert.Equal(t,
		"timeout 90 srun --immediate=30 -p 'worker' -N 2 -n 2 --ntasks-per-node=1 -w 'worker-0,worker-1' --time=1:00 --switches=1 hostname",
		constrained.command())
}

// A tree of "root + one leaf" is valid configuration and holds every worker within one switch, so
// it cannot demonstrate a switch constraint. Counting switches rather than leaves would let it
// through the gate and fail later, on state, instead of skipping here.
func TestLeafSwitchesCountsOnlyTheSwitchesCarryingWorkers(t *testing.T) {
	branching, err := parseTopologyEntries(renderedTopologyConfig)
	require.NoError(t, err)
	require.Len(t, branching, 3)
	assert.Equal(t, 3, leafSwitches(branching[1]))

	singleLeaf, err := parseTopologyEntries(`
- topology: tree-ib
  cluster_default: true
  tree:
    switches:
        - switch: root
          children: leaf-a
        - switch: leaf-a
          nodes: worker-[0-1]
`)
	require.NoError(t, err)
	require.Len(t, singleLeaf, 1)
	assert.Equal(t, 1, leafSwitches(singleLeaf[0]))
}

func TestPickLeafPairs(t *testing.T) {
	leaves := []string{"leaf-a", "leaf-b", "leaf-c"}
	nodesByLeaf := map[string][]string{
		"leaf-a": {"worker-0", "worker-1"},
		"leaf-b": {"worker-2"},
		"leaf-c": {"worker-3"},
	}
	all := func(string) bool { return true }

	t.Run("picks one pair across leaves and one within a leaf", func(t *testing.T) {
		cross, same := pickLeafPairs(leaves, nodesByLeaf, "tree-ib", "worker", all)
		require.NotNil(t, cross)
		assert.Equal(t, [2]string{"worker-0", "worker-2"}, cross.Nodes)
		assert.Equal(t, [2]string{"leaf-a", "leaf-b"}, cross.Leaves)
		require.NotNil(t, same)
		assert.Equal(t, [2]string{"worker-0", "worker-1"}, same.Nodes)
	})

	// The busy cluster the scenario skips on: the tree still separates its workers, none of them
	// can take a job.
	t.Run("reports no pair when the workers it would use are busy", func(t *testing.T) {
		idle := func(node string) bool { return node == "worker-0" || node == "worker-1" }
		cross, same := pickLeafPairs(leaves, nodesByLeaf, "tree-ib", "worker", idle)
		assert.Nil(t, cross)
		require.NotNil(t, same)
		assert.Equal(t, [2]string{"worker-0", "worker-1"}, same.Nodes)
	})

	t.Run("skips a leaf whose workers are all filtered out", func(t *testing.T) {
		idle := func(node string) bool { return node != "worker-2" }
		cross, _ := pickLeafPairs(leaves, nodesByLeaf, "tree-ib", "worker", idle)
		require.NotNil(t, cross)
		assert.Equal(t, [2]string{"worker-0", "worker-3"}, cross.Nodes)
		assert.Equal(t, [2]string{"leaf-a", "leaf-c"}, cross.Leaves)
	})
}

// Ready and Idle answer different questions: a worker running someone else's job is healthy and
// registered, and asking for it would make the scenario wait that job out.
func TestSlurmNodeViewReadyAndIdle(t *testing.T) {
	for _, tc := range []struct {
		state string
		ready bool
		idle  bool
	}{
		{state: "IDLE+CLOUD", ready: true, idle: true},
		{state: "ALLOCATED+CLOUD", ready: true, idle: false},
		{state: "MIXED+CLOUD", ready: true, idle: false},
		{state: "IDLE+CLOUD+POWERED_DOWN", ready: false, idle: false},
		{state: "IDLE+DRAIN", ready: false, idle: false},
		{state: "DOWN+CLOUD", ready: false, idle: false},
	} {
		t.Run(tc.state, func(t *testing.T) {
			view := slurmNodeView{Info: framework.SlurmNodeInfo{Name: "worker-0", State: tc.state}}
			assert.Equal(t, tc.ready, view.Ready())
			assert.Equal(t, tc.idle, view.Idle())
		})
	}
}
