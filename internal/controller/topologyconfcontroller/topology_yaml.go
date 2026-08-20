package topologyconfcontroller

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// topologyYAMLEntry is one entry of topology.yaml.
//
// Field order is significant: Slurm requires "topology" to be the first attribute, which is why
// this file is marshalled with gopkg.in/yaml.v3 (preserves struct field order) rather than
// sigs.k8s.io/yaml (round-trips through JSON and sorts keys alphabetically).
//
// Exactly one of Tree or Block is set, and that choice picks the plugin backing the topology.
// https://slurm.schedmd.com/topology.yaml.html
type topologyYAMLEntry struct {
	Topology       string             `yaml:"topology"`
	ClusterDefault bool               `yaml:"cluster_default"`
	Tree           *treeTopologyYAML  `yaml:"tree,omitempty"`
	Block          *blockTopologyYAML `yaml:"block,omitempty"`
}

type treeTopologyYAML struct {
	Switches []switchYAML `yaml:"switches"`
}

// switchYAML is one switch of a tree topology. Children names child switches, Nodes names child
// worker nodes; a switch carries one or the other, never both.
type switchYAML struct {
	Switch   string `yaml:"switch"`
	Children string `yaml:"children,omitempty"`
	Nodes    string `yaml:"nodes,omitempty"`
}

type blockTopologyYAML struct {
	BlockSizes []int       `yaml:"block_sizes,omitempty"`
	Blocks     []blockYAML `yaml:"blocks"`
}

type blockYAML struct {
	Block string `yaml:"block"`
	Nodes string `yaml:"nodes"`
}

// renderTopologyYAML marshals the entries into the body of topology.yaml.
func renderTopologyYAML(entries []topologyYAMLEntry) (string, error) {
	if len(entries) == 0 {
		return "", nil
	}
	out, err := yaml.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("marshal topology.yaml: %w", err)
	}
	return string(out), nil
}
