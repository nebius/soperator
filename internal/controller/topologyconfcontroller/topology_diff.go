package topologyconfcontroller

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	slurmpattern "nebius.ai/slurm-operator/internal/utils/slurm/pattern"
)

// maxTopologyChangeSummary bounds the summary so it fits an event note, which Kubernetes caps at
// 1024 characters.
const maxTopologyChangeSummary = 800

// topologyChange describes how a re-render moved nodes between topologies.
//
// It exists because the structure fingerprint deliberately omits node membership: moving a node
// from one topology to another changes neither the fingerprint nor the request for a reconfigure,
// so without this the only trace of that edit is the ConfigMap itself.
type topologyChange struct {
	// Summary is short enough for an event note.
	Summary string
	// Detail names the nodes that moved, for the log, where length is not a concern.
	Detail string
}

// topologyMembership maps every topology in a rendered topology.yaml to the nodes it covers.
//
// Flat topologies map to no node: they list none by design, and every node not named elsewhere
// belongs to them.
func topologyMembership(rendered string) (map[string][]string, error) {
	var entries []topologyYAMLEntry
	if err := yaml.Unmarshal([]byte(rendered), &entries); err != nil {
		return nil, fmt.Errorf("unmarshal topology.yaml membership: %w", err)
	}

	membership := make(map[string][]string, len(entries))
	for _, entry := range entries {
		var nodes []string
		switch {
		case entry.Tree != nil:
			for _, networkSwitch := range entry.Tree.Switches {
				nodes = append(nodes, slurmpattern.Expand(networkSwitch.Nodes)...)
			}
		case entry.Block != nil:
			for _, block := range entry.Block.Blocks {
				nodes = append(nodes, slurmpattern.Expand(block.Nodes)...)
			}
		}
		slices.Sort(nodes)
		membership[entry.Topology] = slices.Compact(nodes)
	}

	return membership, nil
}

// summarizeTopologyChange reports how membership differs between two renderings of topology.yaml.
//
// Content that cannot be parsed is reported as such rather than raising an error: the summary is
// there to explain a change that is being published either way, and a hand-edited ConfigMap must
// not stop it.
func summarizeTopologyChange(before, after string) topologyChange {
	beforeMembership, err := topologyMembership(before)
	if err != nil {
		return topologyChange{Summary: "previous topology.yaml could not be parsed"}
	}
	afterMembership, err := topologyMembership(after)
	if err != nil {
		return topologyChange{Summary: "rendered topology.yaml could not be parsed"}
	}

	names := make([]string, 0, len(beforeMembership)+len(afterMembership))
	for name := range beforeMembership {
		names = append(names, name)
	}
	for name := range afterMembership {
		if _, ok := beforeMembership[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)

	var summaries, details []string
	for _, name := range names {
		previous, existed := beforeMembership[name]
		current, remains := afterMembership[name]

		switch {
		case !existed:
			summaries = append(summaries, fmt.Sprintf("+%s (%d nodes)", name, len(current)))
			details = append(details, fmt.Sprintf("+%s=%s", name, slurmpattern.Merge(current)))
		case !remains:
			summaries = append(summaries, fmt.Sprintf("-%s", name))
			details = append(details, fmt.Sprintf("-%s=%s", name, slurmpattern.Merge(previous)))
		case !slices.Equal(previous, current):
			added, removed := membershipDelta(previous, current)
			summaries = append(summaries, fmt.Sprintf("%s %d nodes (+%d -%d)",
				name, len(current), len(added), len(removed)))
			details = append(details, fmt.Sprintf("%s +[%s] -[%s]",
				name, slurmpattern.Merge(added), slurmpattern.Merge(removed)))
		}
	}

	if len(summaries) == 0 {
		// Reached whenever the edit touched something other than membership: a block size, a
		// cluster default, a switch renamed with the same nodes under it.
		return topologyChange{Summary: "node membership unchanged"}
	}

	return topologyChange{
		Summary: truncateSummary(strings.Join(summaries, "; ")),
		Detail:  strings.Join(details, "; "),
	}
}

func membershipDelta(previous, current []string) (added, removed []string) {
	for _, node := range current {
		if !slices.Contains(previous, node) {
			added = append(added, node)
		}
	}
	for _, node := range previous {
		if !slices.Contains(current, node) {
			removed = append(removed, node)
		}
	}
	return added, removed
}

func truncateSummary(summary string) string {
	if len(summary) <= maxTopologyChangeSummary {
		return summary
	}
	return summary[:maxTopologyChangeSummary] + "... (see the operator log for the rest)"
}
