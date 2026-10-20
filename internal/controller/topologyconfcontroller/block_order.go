package topologyconfcontroller

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

type blockIBPath struct {
	path     []topologyTier
	node     string
	conflict bool
}

type ibParent struct {
	switchTier topologyTier
	ambiguous  bool
}

// blockIBPaths derives hardware paths once per reconcile, independent of worker pod lifecycle.
// A NodeSet may span multiple tier-0 blocks; each block has its own path.
func blockIBPaths(labelsByNode map[string]NodeTopologyLabels) map[string]blockIBPath {
	pathsByNode := make(map[string][]topologyTier, len(labelsByNode))
	parents := make(map[topologyTier]ibParent)
	for node, labels := range labelsByNode {
		path, err := labelsToTiers(labels, 1)
		if err != nil || len(path) == 0 {
			continue
		}
		pathsByNode[node] = path
		for i := 1; i < len(path); i++ {
			child, parent := path[i-1], path[i]
			known, ok := parents[child]
			switch {
			case !ok || parent.tier < known.switchTier.tier:
				parents[child] = ibParent{switchTier: parent}
			case parent.tier == known.switchTier.tier && parent.name != known.switchTier.name:
				known.ambiguous = true
				parents[child] = known
			}
		}
	}

	type pathVotes struct {
		path  []topologyTier
		nodes int
		node  string
	}
	votesByBlock := make(map[string]map[string]*pathVotes)
	for node, path := range pathsByNode {
		block := labelsByNode[node]["tier-0"]
		if block == "" {
			continue
		}
		path = completeIBPath(path, parents)
		block = slurmSafeSwitchName(block)
		if votesByBlock[block] == nil {
			votesByBlock[block] = make(map[string]*pathVotes)
		}
		key := formatIBPath(path)
		votes := votesByBlock[block][key]
		if votes == nil {
			votes = &pathVotes{path: path, node: node}
			votesByBlock[block][key] = votes
		}
		votes.nodes++
		votes.node = min(votes.node, node)
	}

	// On a conflict the path most nodes agree on wins, then the most complete one, so a node whose
	// labels are late or partial does not move its whole block.
	pathsByBlock := make(map[string]blockIBPath, len(votesByBlock))
	for block, candidates := range votesByBlock {
		var best *pathVotes
		for _, votes := range candidates {
			if best == nil || cmp.Or(
				cmp.Compare(votes.nodes, best.nodes),
				cmp.Compare(len(votes.path), len(best.path)),
				strings.Compare(best.node, votes.node),
			) > 0 {
				best = votes
			}
		}
		pathsByBlock[block] = blockIBPath{path: best.path, node: best.node, conflict: len(candidates) > 1}
	}
	return pathsByBlock
}

func formatIBPath(path []topologyTier) string {
	tiers := make([]string, 0, len(path))
	for _, tier := range path {
		tiers = append(tiers, fmt.Sprintf("tier-%d=%s", tier.tier, tier.name))
	}
	return strings.Join(tiers, "/")
}

// completeIBPath fills missing ancestors only through unambiguous switch relationships. Explicit
// labels win over inferred ones. Tier numbers increase at every step, so malformed edges cannot cycle.
func completeIBPath(path []topologyTier, parents map[topologyTier]ibParent) []topologyTier {
	completed := make([]topologyTier, 0, len(path))
	current := path[0]
	next := 1
	for {
		completed = append(completed, current)
		parent, known := parents[current]
		known = known && !parent.ambiguous
		if next < len(path) {
			if known && parent.switchTier.tier < path[next].tier {
				current = parent.switchTier
			} else {
				current = path[next]
				next++
			}
		} else if known {
			current = parent.switchTier
		} else {
			break
		}
	}
	slices.Reverse(completed)
	return completed
}

// compareIBPaths orders paths lexicographically from the highest tier down. A missing tier sorts
// after a known one, both in the middle of a path and at its end; names from different tiers are
// never compared.
func compareIBPaths(a, b []topologyTier) int {
	for i := range min(len(a), len(b)) {
		if c := cmp.Compare(b[i].tier, a[i].tier); c != 0 {
			return c
		}
		if c := strings.Compare(a[i].name, b[i].name); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(b), len(a))
}

const maxReportedBlockConflicts = 5

// reportBlockConflicts emits one aggregated log record and event per reconcile, and only when a
// conflict appears or its selected path changes, so a cluster with many inconsistent racks does not
// flood logs and events. A new representative node alone does not count as a change.
func (r *WorkerTopologyReconciler) reportBlockConflicts(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	entries []topologyYAMLEntry,
	pathsByBlock map[string]blockIBPath,
) {
	current := make(map[string]string)
	for _, entry := range entries {
		if entry.Block == nil {
			continue
		}
		for _, block := range entry.Block.Blocks {
			if path := pathsByBlock[block.Block]; path.conflict {
				current[block.Block] = formatIBPath(path.path)
			}
		}
	}

	key := types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}
	r.blockConflictsMu.Lock()
	previous := r.blockConflicts[key]
	if len(current) == 0 {
		delete(r.blockConflicts, key)
	} else {
		if r.blockConflicts == nil {
			r.blockConflicts = make(map[types.NamespacedName]map[string]string)
		}
		r.blockConflicts[key] = current
	}
	r.blockConflictsMu.Unlock()

	var changed []string
	for block, path := range current {
		if previous[block] != path {
			changed = append(changed, block)
		}
	}
	if len(changed) == 0 {
		return
	}
	slices.Sort(changed)
	sampled := min(len(changed), maxReportedBlockConflicts)
	samples := make([]string, 0, sampled)
	for _, block := range changed[:sampled] {
		samples = append(samples, fmt.Sprintf("block %q: Kubernetes node %q, path %s",
			block, pathsByBlock[block].node, current[block]))
	}
	summary := strings.Join(samples, "; ")
	if omitted := len(changed) - len(samples); omitted > 0 {
		summary += fmt.Sprintf("; and %d more", omitted)
	}

	log.FromContext(ctx).WithName(WorkerTopologyReconcilerName).Error(nil,
		"Blocks have conflicting IB topology paths, using the path most of each block's nodes agree on",
		"newConflicts", len(changed), "totalConflicts", len(current), "samples", samples)
	r.recordTopologyIssue(cluster, reasonBlockIBTopologyConflict,
		"%d block(s) have conflicting IB topology paths, using the path most of each block's nodes agree on: %s",
		len(changed), summary)
}

func (r *WorkerTopologyReconciler) forgetBlockConflicts(key types.NamespacedName) {
	r.blockConflictsMu.Lock()
	defer r.blockConflictsMu.Unlock()
	delete(r.blockConflicts, key)
}

// sortBlocksByIBTopology puts the catch-all unknown blocks last, so they never take an index inside
// an aggregate of real racks.
func sortBlocksByIBTopology(blocks []blockYAML, pathsByBlock map[string]blockIBPath) {
	unknownRank := func(name string) int {
		unknown := unknownSwitchName(defaultFabric)
		if name == unknown || strings.HasSuffix(name, "."+unknown) {
			return 1
		}
		return 0
	}
	slices.SortFunc(blocks, func(a, b blockYAML) int {
		return cmp.Or(
			cmp.Compare(unknownRank(a.Block), unknownRank(b.Block)),
			compareIBPaths(pathsByBlock[a.Block].path, pathsByBlock[b.Block].path),
			strings.Compare(a.Block, b.Block),
		)
	})
}
