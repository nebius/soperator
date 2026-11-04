package topology

// FlatTopology represents Slurm topology with type "flat" in topology.yaml format
type FlatTopology bool

func newFlatTopology() *FlatTopology {
	var enabled FlatTopology = true
	return &enabled
}

// changeFrom is ChangeIdentical unless the value differs: "flat: false" selects no plugin, so the
// entry changes kind.
func (t *FlatTopology) changeFrom(previous *FlatTopology) Change {
	if *t != *previous {
		return ChangeRestructured
	}
	return ChangeIdentical
}
