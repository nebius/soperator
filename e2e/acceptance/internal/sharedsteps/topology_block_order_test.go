package sharedsteps

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestValidateBlockNodeRanks(t *testing.T) {
	workers := map[string]int{"rack-z-0": 0, "rack-z-1": 0, "rack-a-0": 2, "rack-a-1": 2}
	const valid = `srun: job allocated
SOPERATOR_NODE_RANK=3:rack-a-0
SOPERATOR_NODE_RANK=1:rack-z-0
SOPERATOR_NODE_RANK=2:rack-a-1
SOPERATOR_NODE_RANK=0:rack-z-1
SOPERATOR_NODE_ID=0:rack-a-0
SOPERATOR_NODE_ID=1:rack-a-1
SOPERATOR_NODE_ID=2:rack-z-0
SOPERATOR_NODE_ID=3:rack-z-1
`
	require.NoError(t, validateBlockNodeRanks(valid, workers))
	for name, output := range map[string]string{
		"alphabetical instead of block order": "SOPERATOR_NODE_RANK=0:rack-a-0\nSOPERATOR_NODE_RANK=1:rack-a-1\nSOPERATOR_NODE_RANK=2:rack-z-0\nSOPERATOR_NODE_RANK=3:rack-z-1",
		"interleaved blocks":                  "SOPERATOR_NODE_RANK=0:rack-z-0\nSOPERATOR_NODE_RANK=1:rack-a-0\nSOPERATOR_NODE_RANK=2:rack-z-1\nSOPERATOR_NODE_RANK=3:rack-a-1",
		"missing rank":                        "SOPERATOR_NODE_RANK=0:rack-z-0",
		"duplicate rank":                      valid + "SOPERATOR_NODE_RANK=0:rack-z-0",
		"duplicate worker":                    "SOPERATOR_NODE_RANK=0:rack-z-0\nSOPERATOR_NODE_RANK=1:rack-z-0",
		"unknown worker":                      "SOPERATOR_NODE_RANK=0:other",
		"out of range rank":                   "SOPERATOR_NODE_RANK=4:rack-z-0",
		"negative rank":                       "SOPERATOR_NODE_RANK=-1:rack-z-0",
		"malformed rank":                      "SOPERATOR_NODE_RANK=bad:rack-z-0",
		"malformed record":                    "SOPERATOR_NODE_RANK=0",
		"no records":                          "",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, validateBlockNodeRanks(output, workers))
		})
	}
}

type blockRankTestRuntime struct {
	framework.Runtime
	controller framework.CommandScope
	jail       framework.CommandScope
	kubectl    framework.ArgsScope
}

func (r blockRankTestRuntime) Controller() framework.CommandScope { return r.controller }
func (r blockRankTestRuntime) Jail() framework.CommandScope       { return r.jail }
func (r blockRankTestRuntime) Kubectl() framework.ArgsScope       { return r.kubectl }
func (r blockRankTestRuntime) Logf(string, ...any)                {}
func (r blockRankTestRuntime) WaitFor(
	ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error),
) error {
	for range 3 {
		if done, err := condition(ctx); done || err != nil {
			return err
		}
	}
	return fmt.Errorf("wait for block order")
}

func TestBlockRankCheckTriesAllPartitions(t *testing.T) {
	for _, tt := range []struct {
		name       string
		partitions []partitionInfo
		idle       bool
		wantSkip   bool
		rerender   bool
	}{
		{
			name:       "topology without a partition does not hide a usable topology",
			partitions: []partitionInfo{{Name: "ready", Topology: "second"}}, idle: true,
		},
		{
			name:       "busy partition does not hide a usable partition",
			partitions: []partitionInfo{{Name: "busy", Topology: "first"}, {Name: "ready", Topology: "second"}}, idle: true,
		},
		{
			name:       "partition without Topology uses cluster default",
			partitions: []partitionInfo{{Name: "ready"}}, idle: true,
		},
		{
			name:       "config order changes while waiting",
			partitions: []partitionInfo{{Name: "ready", Topology: "second"}}, idle: true, rerender: true,
		},
		{
			name:       "all workers busy skips",
			partitions: []partitionInfo{{Name: "busy", Topology: "first"}}, wantSkip: true,
		},
		{
			name:       "no bound partitions skips",
			partitions: []partitionInfo{{Name: "flat", Topology: "flat"}}, wantSkip: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			jobs, topologyReads, configReads := 0, 0, 0
			runtime := blockRankTestRuntime{
				kubectl: framework.NewArgsScope(func(context.Context, ...string) (string, error) {
					configReads++
					first, second := "z", "a"
					if tt.rerender && configReads > 1 {
						first, second = second, first
					}
					return fmt.Sprintf(`- topology: second
  cluster_default: true
  block:
    blocks:
      - block: block-%s
        nodes: worker-%s
      - block: block-%s
        nodes: worker-%s
`, first, first, second, second), nil
				}),
				controller: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
					if command == "scontrol show topology 'second'" {
						return "BlockName=block-z BlockIndex=0 Nodes=worker-z BlockSize=1\nBlockName=block-a BlockIndex=1 Nodes=worker-a BlockSize=1\n", nil
					}
					if command == "scontrol show topoconf" {
						topologyReads++
						first, second := "block-z", "block-a"
						if topologyReads == 1 || tt.rerender {
							first, second = second, first
						}
						return fmt.Sprintf("- topology: second\n  block:\n    blocks:\n      - block: %s\n      - block: %s\n", first, second), nil
					}
					if tt.idle && strings.Contains(command, "--partition='ready'") {
						return "worker-z idle\nworker-a idle\n", nil
					}
					return "worker-z allocated\nworker-a allocated\n", nil
				}),
				jail: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
					jobs++
					assert.Contains(t, command, "-p 'ready'")
					if tt.rerender {
						return "SOPERATOR_NODE_RANK=0:worker-a\nSOPERATOR_NODE_RANK=1:worker-z\n", nil
					}
					return "SOPERATOR_NODE_RANK=1:worker-a\nSOPERATOR_NODE_RANK=0:worker-z\n", nil
				}),
			}
			s := &Topology{info: &framework.ClusterInfo{SlurmClusterName: "cluster"}, runtime: runtime, partitions: tt.partitions, rendered: []topologyEntry{
				{Name: "first", Kind: topologyKindBlock, Blocks: []topologyUnit{{Name: "block-z", Nodes: "worker-z"}, {Name: "block-a", Nodes: "worker-a"}}},
				{Name: "second", Kind: topologyKindBlock, ClusterDefault: true, Blocks: []topologyUnit{{Name: "block-z", Nodes: "worker-z"}, {Name: "block-a", Nodes: "worker-a"}}},
			}}
			err := s.blockNodeRanksFollowConfig(t.Context())
			if tt.wantSkip {
				assert.ErrorIs(t, err, godog.ErrSkip)
				assert.Zero(t, jobs)
			} else {
				require.NoError(t, err)
				assert.Equal(t, 1, jobs)
				assert.Equal(t, 2, topologyReads, "wait for Slurm to load the new order")
			}
		})
	}
}

func TestPickBlockRankWorkersRespectsAllocationGroups(t *testing.T) {
	for _, tt := range []struct {
		name       string
		sizes      []int
		base       int
		idleBlocks []int
		workers    int
		wantNodes  int
	}{
		{name: "GB300 pair needs more than a base block", sizes: []int{18, 36}, idleBlocks: []int{0, 1}, workers: 18, wantNodes: 19},
		{name: "two idle nodes are insufficient", sizes: []int{18, 36}, idleBlocks: []int{0, 1}, workers: 1},
		{name: "adjacent blocks across an aggregate boundary cannot share a job", sizes: []int{18, 36}, idleBlocks: []int{1, 2}, workers: 18},
		{name: "a larger aggregate allows nonadjacent blocks", sizes: []int{18, 72}, idleBlocks: []int{0, 2}, workers: 18, wantNodes: 19},
		{name: "later aggregate can be used", sizes: []int{18, 36}, idleBlocks: []int{2, 3}, workers: 18, wantNodes: 19},
		{name: "base only falls back to the whole topology above base size", sizes: []int{18}, idleBlocks: []int{1, 2}, workers: 18, wantNodes: 19},
		{name: "unknown block cannot be selected", sizes: []int{18, 36}, idleBlocks: []int{4, 5}, workers: 18},
		{name: "implicit sizes use the base size Slurm loaded", base: 2, idleBlocks: []int{0, 1}, workers: 4, wantNodes: 3},
		{name: "oversized job skips", sizes: []int{64, 128}, idleBlocks: []int{0, 1}, workers: 64},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var blocks []topologyUnit
			idle := make(map[string]bool)
			for i := range 6 {
				var nodes []string
				for j := range tt.workers {
					node := fmt.Sprintf("worker-%d-%d", i, j)
					nodes = append(nodes, node)
					if slices.Contains(tt.idleBlocks, i) {
						idle[node] = true
					}
				}
				name := fmt.Sprintf("block-%d", i)
				if i == 5 {
					name = "fabric.unknown"
				}
				blocks = append(blocks, topologyUnit{Name: name, Nodes: strings.Join(nodes, ",")})
			}
			base := tt.base
			if base == 0 {
				base = tt.sizes[0]
			}
			nodes, blockByWorker, err := pickBlockRankWorkers(topologyEntry{Blocks: blocks, BlockSizes: tt.sizes}, base, idle,
				func(string) ([]string, error) { t.Fatal("plain hostlists need no exec"); return nil, nil })
			require.NoError(t, err)
			require.Len(t, nodes, tt.wantNodes)
			counts := make(map[int]int)
			for _, node := range nodes {
				assert.True(t, idle[node])
				counts[blockByWorker[node]]++
			}
			if tt.wantNodes > 0 {
				assert.Len(t, counts, 2)
				assert.NotContains(t, counts, 5)
			}
		})
	}
}

func TestExpandBlockHostlistsBatchesTenThousandWorkers(t *testing.T) {
	var blocks []topologyUnit
	var output []string
	for i := range 500 {
		blocks = append(blocks, topologyUnit{Name: fmt.Sprintf("block-%d", i), Nodes: fmt.Sprintf("worker-%d-[0-19]", i)})
		output = append(output, fmt.Sprintf("__SOPERATOR_BLOCK_%d__", i))
		for j := range 20 {
			output = append(output, fmt.Sprintf("worker-%d-%d", i, j))
		}
	}
	calls := 0
	nodes, err := expandBlockHostlists(blocks, func(value string) ([]string, error) {
		calls++
		assert.Contains(t, value, "worker-0-[0-19]")
		assert.Contains(t, value, "worker-499-[0-19]")
		return output, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	require.Len(t, nodes, 500)
	for i, workers := range nodes {
		require.Len(t, workers, 20)
		assert.Equal(t, fmt.Sprintf("worker-%d-0", i), workers[0])
		assert.Equal(t, fmt.Sprintf("worker-%d-19", i), workers[19])
	}
}

func TestPartitionsAreBoundResolvesClusterDefault(t *testing.T) {
	partitions := []partitionInfo{{Name: "main"}, {Name: "blocks", Topology: "blocks"}}
	s := &Topology{partitions: partitions, loaded: []topologyEntry{
		{Name: "tree", Kind: topologyKindTree, ClusterDefault: true},
		{Name: "blocks", Kind: topologyKindBlock},
	}}
	require.NoError(t, s.partitionsAreBound(t.Context()))

	s.loaded[0].ClusterDefault = false
	assert.ErrorContains(t, s.partitionsAreBound(t.Context()), "main is bound to no topology and no cluster default is loaded")
}

func TestParseNodeTierLabels(t *testing.T) {
	out := "node-a\t" + `{"kubernetes.io/hostname":"node-a","topology.nebius.com/tier-0":"rack-a","topology.nebius.com/tier-1":"leaf-a","topology.nebius.com/tier-bad":"x"}` + "\n\n"
	tiers, err := parseNodeTierLabels(out)
	require.NoError(t, err)
	assert.Equal(t, map[string]map[int]string{"node-a": {0: "rack-a", 1: "leaf-a"}}, tiers)

	_, err = parseNodeTierLabels("node-a\tnot-json")
	assert.Error(t, err)
}

func TestValidateBlockIBAdjacency(t *testing.T) {
	// Two spines; leaf-1 and leaf-2 under spine-a, leaf-3 under spine-b. rack-a and rack-b share
	// leaf-1. rack-rail has nodes on different leaves (rail-optimized) but one spine.
	tiersByNode := map[string]map[int]string{
		"a0":    {0: "rack-a", 1: "leaf-1", 2: "spine-a"},
		"a1":    {0: "rack-a", 1: "leaf-1", 2: "spine-a"},
		"b0":    {0: "rack-b", 1: "leaf-1", 2: "spine-a"},
		"c0":    {0: "rack-c", 1: "leaf-2", 2: "spine-a"},
		"d0":    {0: "rack-d", 1: "leaf-3", 2: "spine-b"},
		"rail0": {0: "rack-rail", 1: "leaf-4", 2: "spine-b"},
		"rail1": {0: "rack-rail", 1: "leaf-5", 2: "spine-b"},
		"gap0":  {0: "rack-gap", 2: "spine-a"},
		"bare0": {0: "rack-bare"},
		"long0": {0: "6f84b74219aa22869602735141708147", 1: "leaf-3", 2: "spine-b"},
	}
	groups := blockIBGroups([]string{"rack-a", "rack-rail"}, tiersByNode)
	assert.Len(t, groups[1], 1, "a rail-optimized rack has no single tier-1 switch")
	assert.Len(t, groups[2], 2)

	valid := []string{"rack-a", "rack-b", "rack-c", "rack-gap", "rack-d", "6f84b74219aa22869602735141708147_", "rack-rail", "rack-bare", "fabric.unknown"}
	require.NoError(t, validateBlockIBAdjacency(valid, tiersByNode))

	for name, tt := range map[string]struct {
		order []string
		want  string
	}{
		"blocks of one leaf split": {
			order: []string{"rack-a", "rack-c", "rack-b", "rack-d"},
			want:  "block rack-b is separated from the other blocks under tier-1 chain leaf-1/spine-a",
		},
		"blocks of one spine split": {
			order: []string{"rack-a", "rack-b", "rack-d", "rack-c"},
			want:  "block rack-c is separated from the other blocks under tier-2 chain spine-a",
		},
		"blocks of one spine split by a rail-optimized rack": {
			order: []string{"rack-d", "rack-a", "rack-rail"},
			want:  "block rack-rail is separated from the other blocks under tier-2 chain spine-b",
		},
		"unlabelled block before a labelled one": {
			order: []string{"rack-bare", "rack-a"},
			want:  "labelled block rack-a follows block rack-bare without IB labels",
		},
		"unknown block before a real one": {
			order: []string{"unknown", "rack-a"},
			want:  "block rack-a follows unknown block unknown",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.ErrorContains(t, validateBlockIBAdjacency(tt.order, tiersByNode), tt.want)
		})
	}
}
