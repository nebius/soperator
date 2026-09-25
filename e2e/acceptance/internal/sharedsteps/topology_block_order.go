package sharedsteps

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func (s *Topology) clusterIsConfiguredWithMultipleBlocks(ctx context.Context) error {
	topologies, err := s.configuredTopologies(ctx)
	if err != nil {
		return err
	}
	var blockTopologies []string
	for _, topology := range topologies {
		if topology.Topo.Type == topologyKindBlock {
			blockTopologies = append(blockTopologies, topology.Name)
		}
	}
	if len(blockTopologies) > 0 {
		if err := s.loadRenderedConfig(ctx); err != nil {
			return err
		}
	}
	for _, name := range blockTopologies {
		entry, ok := topologyByName(s.rendered, name)
		if !ok {
			return fmt.Errorf("find configured block topology %q in the published config", name)
		}
		populated := 0
		for _, block := range entry.Blocks {
			if block.Nodes != "" && !isUnknownTopologyUnit(block.Name) {
				populated++
			}
		}
		if populated >= 2 {
			return nil
		}
	}
	s.runtime.Logf("acceptance: no configured topology has multiple populated blocks, skipping node rank check")
	return godog.ErrSkip
}

func topologyForPartition(entries []topologyEntry, name string) (topologyEntry, bool) {
	if name != "" {
		return topologyByName(entries, name)
	}
	for _, entry := range entries {
		if entry.ClusterDefault {
			return entry, true
		}
	}
	return topologyEntry{}, false
}

func (s *Topology) blockNodeRanksFollowConfig(ctx context.Context) error {
	if err := s.loadPartitions(ctx); err != nil {
		return err
	}
	for _, partition := range s.partitions {
		entry, ok := topologyForPartition(s.rendered, partition.Topology)
		if !ok || entry.Kind != topologyKindBlock {
			continue
		}
		idle, err := s.idleWorkers(ctx, partition.Name)
		if err != nil {
			return err
		}
		if len(idle) < 2 {
			continue
		}
		entry, err = s.waitForBlockOrder(ctx, partition.Topology)
		if err != nil {
			return err
		}
		if entry.Kind != topologyKindBlock {
			continue
		}
		// The wait can take minutes, so the workers idle before it may be busy now.
		if idle, err = s.idleWorkers(ctx, partition.Name); err != nil {
			return err
		}
		base, err := s.loadedBaseBlockSize(ctx, entry.Name)
		if err != nil {
			return err
		}
		selected, blockByWorker, err := pickBlockRankWorkers(entry, base, idle, func(value string) ([]string, error) {
			return s.expandHostlist(ctx, value)
		})
		if err != nil {
			return err
		}
		if len(selected) == 0 {
			s.runtime.Logf("acceptance: partition %s has no pair of blocks with enough idle workers for a rank check of at most %d nodes (blockSizes=%v)",
				partition.Name, maxBlockRankNodes, entry.BlockSizes)
			continue
		}
		// The command's hostlist order must not supply the ordering being tested.
		slices.Reverse(selected)
		output, err := s.srun(ctx, srunRequest{
			Partition: partition.Name, Nodes: selected,
			Immediate: topologyJobImmediate, ReportNodeRanks: true,
		})
		if err != nil {
			return fmt.Errorf("run node rank check in partition %s: %w", partition.Name, err)
		}
		return validateBlockNodeRanks(output, blockByWorker)
	}
	s.runtime.Logf("acceptance: no block partition has enough idle workers in compatible blocks, skipping node rank check")
	return godog.ErrSkip
}

const maxBlockRankNodes = 64

func (s *Topology) idleWorkers(ctx context.Context, partition string) (map[string]bool, error) {
	out, err := s.runtime.Controller().RunWithDefaultRetry(ctx,
		"sinfo --Node --noheader --partition="+framework.ShellQuote(partition)+" --format='%N %T'")
	if err != nil {
		return nil, fmt.Errorf("read idle workers of partition %s: %w", partition, err)
	}
	idle := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "idle" {
			idle[fields[0]] = true
		}
	}
	return idle, nil
}

// loadedBaseBlockSize returns the base block size slurmctld fixed at its last read of the file.
// Without block_sizes Slurm takes it from the first populated block at that time, and node list
// changes since then do not update it, so the published config cannot tell it.
func (s *Topology) loadedBaseBlockSize(ctx context.Context, topology string) (int, error) {
	blocks, err := s.slurmBlocksOf(ctx, topology)
	if err != nil {
		return 0, err
	}
	return blocks[0].Size, nil
}

func pickBlockRankWorkers(
	entry topologyEntry, base int, idle map[string]bool, expand hostlistExpander,
) ([]string, map[string]int, error) {
	if base <= 0 || base >= maxBlockRankNodes || len(idle) <= base {
		return nil, nil, nil
	}
	nodesByBlock, err := expandBlockHostlists(entry.Blocks, expand)
	if err != nil {
		return nil, nil, err
	}
	// Slurm chooses the allocation level from job size. A job with base+1 nodes can
	// span two base blocks inside the smallest configured aggregate. With no larger
	// configured size, eval_nodes_block uses the whole topology as the allocation group.
	span := len(entry.Blocks)
	if len(entry.BlockSizes) == 0 {
		span = 2
	} else {
		for _, size := range entry.BlockSizes {
			if size > base {
				span = min(span, size/base)
			}
		}
	}
	for start := 0; start < len(entry.Blocks); start += span {
		var best []string
		bestIndex := start
		for index := start; index < min(start+span, len(entry.Blocks)); index++ {
			if isUnknownTopologyUnit(entry.Blocks[index].Name) {
				continue
			}
			var available []string
			for _, node := range nodesByBlock[index] {
				if idle[node] {
					available = append(available, node)
					if len(available) == base {
						break
					}
				}
			}
			if len(best)+len(available) >= base+1 {
				selected := append(slices.Clone(best), available[:base+1-len(best)]...)
				blockByWorker := make(map[string]int, len(selected))
				for i, node := range selected {
					blockByWorker[node] = bestIndex
					if i >= len(best) {
						blockByWorker[node] = index
					}
				}
				return selected, blockByWorker, nil
			}
			if len(available) > len(best) {
				best, bestIndex = available, index
			}
		}
	}
	return nil, nil, nil
}

// expandBlockHostlists expands all ranged lists in one controller call. Separator hosts
// preserve block boundaries in scontrol's output; they are never submitted to srun.
func expandBlockHostlists(blocks []topologyUnit, expand hostlistExpander) ([][]string, error) {
	nodes := make([][]string, len(blocks))
	var lists []string
	markers := make(map[string]int)
	for i, block := range blocks {
		if plain, ok := plainHostlist(block.Nodes); ok {
			nodes[i] = plain
			continue
		}
		marker := fmt.Sprintf("__SOPERATOR_BLOCK_%d__", i)
		markers[marker] = i
		lists = append(lists, marker, block.Nodes)
	}
	if len(lists) == 0 {
		return nodes, nil
	}
	expanded, err := expand(strings.Join(lists, ","))
	if err != nil {
		return nil, err
	}
	index := -1
	for _, node := range expanded {
		if boundary, ok := markers[node]; ok {
			index = boundary
		} else if index >= 0 {
			nodes[index] = append(nodes[index], node)
		} else {
			return nil, fmt.Errorf("expand block hostlists: missing block boundary before %q", node)
		}
	}
	for _, i := range markers {
		if len(nodes[i]) == 0 {
			return nil, fmt.Errorf("expand hostlist of block %q: no workers returned", blocks[i].Name)
		}
	}
	return nodes, nil
}

func (s *Topology) waitForBlockOrder(ctx context.Context, topology string) (topologyEntry, error) {
	var expected topologyEntry
	err := s.runtime.WaitFor(ctx,
		"Slurm to load the currently published block order",
		topologyConvergeTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			_, rendered, err := s.readTopologyConfigMap(waitCtx)
			if err != nil {
				return false, err
			}
			s.rendered, err = parseTopologyEntries(rendered)
			if err != nil {
				return false, err
			}
			var ok bool
			expected, ok = topologyForPartition(s.rendered, topology)
			// A topology gone from the published config, or no longer a block one, leaves nothing to
			// wait for; the caller moves on to the next partition.
			if !ok || expected.Kind != topologyKindBlock {
				return true, nil
			}
			if err := s.readLoadedTopologies(waitCtx); err != nil {
				return false, err
			}
			loaded, ok := topologyByName(s.loaded, expected.Name)
			return ok && slices.EqualFunc(expected.Blocks, loaded.Blocks, func(a, b topologyUnit) bool {
				return a.Name == b.Name
			}), nil
		})
	return expected, err
}

// validateBlockNodeRanks checks task ranks with one task per node. Slurm's per-job topology
// ranking orders tasks (SLURM_PROCID), while SLURM_NODEID is the index in the node list.
// Output arrival order and ranks within a block are unconstrained.
func validateBlockNodeRanks(output string, blockByWorker map[string]int) error {
	blockByRank := make([]int, len(blockByWorker))
	seenRanks := make(map[int]bool, len(blockByWorker))
	seenWorkers := make(map[string]bool, len(blockByWorker))
	for _, line := range strings.Split(output, "\n") {
		record, ok := strings.CutPrefix(strings.TrimSpace(line), "SOPERATOR_NODE_RANK=")
		if !ok {
			continue
		}
		rankText, worker, ok := strings.Cut(record, ":")
		if !ok {
			return fmt.Errorf("parse node rank record %q", line)
		}
		rank, err := strconv.Atoi(rankText)
		if err != nil || rank < 0 || rank >= len(blockByWorker) {
			return fmt.Errorf("parse SLURM_PROCID %q of worker %q: expected an integer in [0, %d)",
				rankText, worker, len(blockByWorker))
		}
		block, ok := blockByWorker[worker]
		if !ok {
			return fmt.Errorf("match worker %q from node rank output to the selected workers", worker)
		}
		if seenRanks[rank] || seenWorkers[worker] {
			return fmt.Errorf("parse node rank record %q: duplicate rank or worker", line)
		}
		seenRanks[rank], seenWorkers[worker] = true, true
		blockByRank[rank] = block
	}
	if len(seenWorkers) != len(blockByWorker) {
		return fmt.Errorf("collect node ranks from %d workers, expected %d", len(seenWorkers), len(blockByWorker))
	}
	for rank := 1; rank < len(blockByRank); rank++ {
		if blockByRank[rank] < blockByRank[rank-1] {
			return fmt.Errorf("check rank order: SLURM_PROCID %d belongs to block index %d after block index %d",
				rank, blockByRank[rank], blockByRank[rank-1])
		}
	}
	return nil
}
