// Package topology turns NodeSets and their nodes' network paths into ordered Slurm
// topologies, independent of Kubernetes objects and of the topology.yaml format. The order of leaf
// switches and blocks, which Slurm ranks nodes by, is decided here once for every topology.
package topology

import "fmt"

// MultiTopology is full Slurm network topology configuration in topology.yaml format.
type MultiTopology []SlurmTopology

// SlurmTopology is one built topology in topology.yaml format. Exactly one of Flat, Tree and Block is set.
type SlurmTopology struct {
	Name           string         `yaml:"topology"`
	ClusterDefault bool           `yaml:"cluster_default"`
	Flat           *FlatTopology  `yaml:"flat,omitempty"`
	Tree           *TreeTopology  `yaml:"tree,omitempty"`
	Block          *BlockTopology `yaml:"block,omitempty"`
}

// IsEmpty reports whether a tree or block topology lists no node. A flat topology lists none by
// design and is never empty.
func (t SlurmTopology) IsEmpty() bool {
	switch {
	case t.Tree != nil:
		for _, sw := range t.Tree.Switches {
			if sw.Nodes != "" {
				return false
			}
		}
		return true
	case t.Block != nil:
		for _, block := range t.Block.Blocks {
			if block.Nodes != "" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// Change is what slurmctld needs to pick up the difference between two topologies.
type Change string

const (
	// ChangeIdentical: no changes.
	ChangeIdentical Change = "Identical"
	// ChangeRegistered: a small difference that slurmd dynamic topology registration can carry.
	ChangeRegistered Change = "Registered"
	// ChangeReordered: slurmctld needs to re-read the topology file, but only for node ranks to match.
	ChangeReordered Change = "Reordered"
	// ChangeRestructured: slurmctld needs to re-read the topology before nodes registered or they are rejected.
	ChangeRestructured Change = "Restructured"
)

// changeRank orders the changes by how much they need from slurmctld.
var changeRank = map[Change]int{
	ChangeIdentical:    0,
	ChangeRegistered:   1,
	ChangeReordered:    2,
	ChangeRestructured: 3,
}

func strongestChange(a, b Change) Change {
	if changeRank[b] > changeRank[a] {
		return b
	}
	return a
}

// NewMultiTopology builds every topology of the cluster from one network forest.
//
// Every node of every NodeSet is listed, scheduled or not; a node without a path goes to its
// fabric's unknown leaf or block. Leaves are named after the node's lowest tier, so a leaf has the
// same name in every topology, and every topology lists its leaves in the same order.
func NewMultiTopology(topologySpecs []Spec, nodeSets []NodeSet) (*MultiTopology, error) {
	if err := validateSpecs(topologySpecs); err != nil {
		return nil, err
	}
	if err := validateNodeSets(nodeSets); err != nil {
		return nil, err
	}

	// Build a single forest representation of the whole cluster's network topology.
	forest, err := newNetworkForest(nodeSets)
	if err != nil {
		return nil, fmt.Errorf("build network forest: %w", err)
	}

	var topologies MultiTopology
	for _, spec := range topologySpecs {
		topology := SlurmTopology{
			Name:           spec.name,
			ClusterDefault: spec.clusterDefault,
		}
		switch spec.kind {
		case topologyTypeFlat:
			topology.Flat = newFlatTopology()
		case topologyTypeTree:
			subforest := forest.cutOutNodeSets(spec.nodeSetNames)
			topology.Tree = newTreeTopology(subforest)
		case topologyTypeBlock:
			subforest := forest.cutOutNodeSets(spec.nodeSetNames)
			topology.Block = newBlockTopology(subforest, spec.blockSizes)
		}
		topologies = append(topologies, topology)
	}
	return &topologies, nil
}

func validateSpecs(specs []Spec) error {
	seen := make(map[string]bool, len(specs))
	defaults := 0
	for _, spec := range specs {
		if seen[spec.name] {
			return fmt.Errorf("validate topology spec %q: name is declared twice", spec.name)
		}
		seen[spec.name] = true
		if spec.kind == topologyTypeBlock && len(spec.blockSizes) == 0 {
			return fmt.Errorf("validate topology spec %q: block sizes are missing", spec.name)
		}
		if spec.clusterDefault {
			defaults++
		}
	}
	if defaults != 1 {
		return fmt.Errorf("validate topology specs: expected exactly one cluster default, found %d", defaults)
	}
	return nil
}

func validateNodeSets(nodeSets []NodeSet) error {
	seen := make(map[string]bool, len(nodeSets))
	for _, nodeSet := range nodeSets {
		if seen[nodeSet.name] {
			return fmt.Errorf("validate NodeSet %q: name is declared twice", nodeSet.name)
		}
		seen[nodeSet.name] = true
	}
	return nil
}

// ChangeFrom matches entries by name, entry order aside. An entry added, removed, changing kind or
// changing the cluster default is ChangeRestructured; otherwise it is the strongest change of an entry.
func (t *MultiTopology) ChangeFrom(previous *MultiTopology) Change {
	if t == nil {
		t = new(MultiTopology)
	}
	if previous == nil {
		previous = new(MultiTopology)
	}
	previousByName := make(map[string]SlurmTopology, len(*previous))
	for _, topology := range *previous {
		previousByName[topology.Name] = topology
	}
	if len(previousByName) != len(*t) {
		return ChangeRestructured
	}

	change := ChangeIdentical
	for _, topology := range *t {
		before, ok := previousByName[topology.Name]
		if !ok || before.ClusterDefault != topology.ClusterDefault {
			return ChangeRestructured
		}
		switch {
		case topology.Flat != nil && before.Flat != nil:
			change = strongestChange(change, topology.Flat.changeFrom(before.Flat))
		case topology.Tree != nil && before.Tree != nil:
			change = strongestChange(change, topology.Tree.changeFrom(before.Tree))
		case topology.Block != nil && before.Block != nil:
			change = strongestChange(change, topology.Block.changeFrom(before.Block))
		default:
			return ChangeRestructured
		}
	}
	return change
}
