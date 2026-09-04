package topologyconfcontroller

import (
	"fmt"
	"slices"
	"strings"

	slurmpattern "nebius.ai/slurm-operator/internal/utils/slurm/pattern"
)

// unknownSwitchSuffix marks the catch-all unit a node lands in while its tier labels are still
// missing. Registrations are never pushed for it: the catch-all is a statement that the placement
// is not known yet, and pinning it into slurmctld would assert an answer the operator does not have.
const unknownSwitchSuffix = "unknown"

// desiredRegistrations maps every node the rendered topology.yaml places to the registration string
// that puts it there -- the same value a worker computes for itself in its init container.
//
// The format is Slurm's dynamic topology syntax: comma-separated "<name>:<unit>" specs, where a
// tree unit is the switch path from the root down to the node's leaf and a block unit is the block
// name. Flat topologies are left out: they list no node, so there is nothing to register into.
//
// https://slurm.schedmd.com/topology.html#dynamic_topo
func desiredRegistrations(rendered string) (map[string]string, error) {
	entries, err := parseTopologyEntries(rendered)
	if err != nil {
		return nil, err
	}

	specs := make(map[string][]string)
	for _, entry := range entries {
		var units map[string]string
		switch {
		case entry.Tree != nil:
			units, err = treeUnits(entry)
			if err != nil {
				return nil, err
			}
		case entry.Block != nil:
			units = blockUnits(entry)
		default:
			continue
		}

		for node, unit := range units {
			specs[node] = append(specs[node], fmt.Sprintf("%s:%s", entry.Topology, unit))
		}
	}

	registrations := make(map[string]string, len(specs))
	for node, nodeSpecs := range specs {
		registrations[node] = strings.Join(nodeSpecs, ",")
	}
	return registrations, nil
}

// treeUnits maps each node of a tree topology to its switch path, root first, leaf last.
func treeUnits(entry topologyYAMLEntry) (map[string]string, error) {
	parents := make(map[string]string)
	for _, networkSwitch := range entry.Tree.Switches {
		for _, child := range slurmpattern.Expand(networkSwitch.Children) {
			parents[child] = networkSwitch.Switch
		}
	}

	units := make(map[string]string)
	for _, networkSwitch := range entry.Tree.Switches {
		if networkSwitch.Nodes == "" || isUnknownUnit(networkSwitch.Switch) {
			continue
		}
		path, err := switchPath(networkSwitch.Switch, parents)
		if err != nil {
			return nil, fmt.Errorf("topology %q: %w", entry.Topology, err)
		}
		for _, node := range slurmpattern.Expand(networkSwitch.Nodes) {
			units[node] = path
		}
	}
	return units, nil
}

// switchPath walks from a leaf up to the root and returns the switches root first.
func switchPath(leaf string, parents map[string]string) (string, error) {
	path := []string{leaf}
	seen := map[string]struct{}{leaf: {}}

	for current := leaf; ; {
		parent, ok := parents[current]
		if !ok {
			break
		}
		if _, looped := seen[parent]; looped {
			return "", fmt.Errorf("switch %q sits in a cycle", leaf)
		}
		seen[parent] = struct{}{}
		path = append(path, parent)
		current = parent
	}

	slices.Reverse(path)
	return strings.Join(path, ":"), nil
}

func blockUnits(entry topologyYAMLEntry) map[string]string {
	units := make(map[string]string)
	for _, block := range entry.Block.Blocks {
		if block.Nodes == "" || isUnknownUnit(block.Block) {
			continue
		}
		for _, node := range slurmpattern.Expand(block.Nodes) {
			units[node] = block.Block
		}
	}
	return units
}

func isUnknownUnit(name string) bool {
	return name == unknownSwitchSuffix || strings.HasSuffix(name, "."+unknownSwitchSuffix)
}
