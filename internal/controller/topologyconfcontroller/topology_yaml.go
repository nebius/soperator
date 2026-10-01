package topologyconfcontroller

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const blockOrderFingerprintPrefix = ":blocks="

// topologyYAMLEntry is one entry of topology.yaml.
//
// Field order is significant: Slurm requires "topology" to be the first attribute, which is why
// this file is marshalled with gopkg.in/yaml.v3 (preserves struct field order) rather than
// sigs.k8s.io/yaml (round-trips through JSON and sorts keys alphabetically).
//
// Exactly one of Tree, Block or Flat is set, and that choice picks the plugin backing the topology.
// Flat is never selected by the user: the operator generates it for CPU-only NodeSets.
// https://slurm.schedmd.com/topology.yaml.html
type topologyYAMLEntry struct {
	Topology       string             `yaml:"topology"`
	ClusterDefault bool               `yaml:"cluster_default"`
	Tree           *treeTopologyYAML  `yaml:"tree,omitempty"`
	Block          *blockTopologyYAML `yaml:"block,omitempty"`
	Flat           bool               `yaml:"flat,omitempty"`
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

// blockYAML is one block. Nodes is omitted rather than emitted empty when the block has none:
// Slurm checks the field for presence, so an empty string would be parsed as a node list and
// rejected as an invalid node name.
type blockYAML struct {
	Block string `yaml:"block"`
	Nodes string `yaml:"nodes,omitempty"`
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

// topologyStructure fingerprints the rendered settings that slurmctld can only learn by
// re-reading topology.yaml, including the ordered block names of block topologies: slurmctld takes
// the block order from the file and rejects a node registering into a block it has not loaded.
// Node lists are omitted because workers push their placement into the running controller.
func topologyStructure(rendered string) (string, error) {
	var entries []topologyYAMLEntry
	if err := yaml.Unmarshal([]byte(rendered), &entries); err != nil {
		return "", fmt.Errorf("unmarshal topology.yaml structure: %w", err)
	}

	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		var blockSizes []int
		if entry.Block != nil {
			blockSizes = entry.Block.BlockSizes
		}
		part := fmt.Sprintf("%s=%s:%v:%t", entry.Topology, topologyPlugin(entry), blockSizes, entry.ClusterDefault)
		if entry.Block != nil {
			var names []string
			for _, block := range entry.Block.Blocks {
				names = append(names, block.Block)
			}
			// A fixed-size hash keeps the annotation bounded on large clusters.
			part += fmt.Sprintf("%s%x", blockOrderFingerprintPrefix, sha256.Sum256([]byte(strings.Join(names, "\x00"))))
		}
		parts = append(parts, part)
	}

	return strings.Join(parts, ","), nil
}

func topologyPlugin(entry topologyYAMLEntry) string {
	switch {
	case entry.Tree != nil:
		return "tree"
	case entry.Block != nil:
		return "block"
	case entry.Flat:
		return "flat"
	}
	return ""
}

// deferrableTopologyChange reports whether desired differs from published only in block order or in
// blocks that disappeared. Until slurmctld re-reads the file, a stale order only worsens placement
// and a vanished block stays loaded but harmless, so such a change can wait. A new block cannot:
// slurmctld drains a node that registers into a block it has not loaded.
func deferrableTopologyChange(published, desired string) bool {
	var before, after []topologyYAMLEntry
	if yaml.Unmarshal([]byte(published), &before) != nil || yaml.Unmarshal([]byte(desired), &after) != nil {
		return false
	}
	if len(before) != len(after) {
		return false
	}

	changed := false
	for i := range after {
		b, a := before[i], after[i]
		if a.Topology != b.Topology || a.ClusterDefault != b.ClusterDefault || topologyPlugin(a) != topologyPlugin(b) {
			return false
		}
		if (a.Block == nil) != (b.Block == nil) {
			return false
		}
		// Only block changes may wait: holding back anything else would leave it out of the file
		// slurmctld reads on its next restart.
		if a.Block == nil {
			if !reflect.DeepEqual(a, b) {
				return false
			}
			continue
		}
		if !slices.Equal(a.Block.BlockSizes, b.Block.BlockSizes) {
			return false
		}
		known := make(map[string]struct{}, len(b.Block.Blocks))
		for _, block := range b.Block.Blocks {
			known[block.Block] = struct{}{}
		}
		for j, block := range a.Block.Blocks {
			if _, ok := known[block.Block]; !ok {
				return false
			}
			changed = changed || j >= len(b.Block.Blocks) || block.Block != b.Block.Blocks[j].Block
		}
		changed = changed || len(a.Block.Blocks) != len(b.Block.Blocks)
	}
	return changed
}
