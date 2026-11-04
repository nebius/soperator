package topology

import (
	"fmt"
	"slices"
)

type topologyType string

const (
	topologyTypeFlat  topologyType = "flat"
	topologyTypeTree  topologyType = "tree"
	topologyTypeBlock topologyType = "block"
)

// Spec represents configuration of one desired Slurm topology.
type Spec struct {
	name           string
	kind           topologyType
	clusterDefault bool
	blockSizes     []int
	nodeSetNames   []string
}

// NewSpec creates and validates a topology spec. kind is "flat", "tree" or "block".
// blockSizes are only relevant for "block" kind, each a power-of-two multiple of the previous.
func NewSpec(name, kind string, clusterDefault bool, blockSizes []int, nodeSetNames []string) (Spec, error) {
	if name == "" {
		return Spec{}, fmt.Errorf("validate topology spec: name is empty")
	}
	spec := Spec{
		name:           name,
		kind:           topologyType(kind),
		clusterDefault: clusterDefault,
		blockSizes:     slices.Clone(blockSizes),
		nodeSetNames:   slices.Clone(nodeSetNames),
	}
	switch spec.kind {
	case topologyTypeFlat, topologyTypeTree:
		if len(blockSizes) > 0 {
			return Spec{}, fmt.Errorf("validate topology spec %q: block sizes are only for block topologies", name)
		}
	case topologyTypeBlock:
		if err := validateBlockSizes(blockSizes); err != nil {
			return Spec{}, fmt.Errorf("validate topology spec %q: %w", name, err)
		}
	default:
		return Spec{}, fmt.Errorf("validate topology spec %q: unsupported type %q", name, kind)
	}
	return spec, nil
}

func (s Spec) Name() string {
	return s.name
}

func validateBlockSizes(sizes []int) error {
	for i, size := range sizes {
		if size <= 0 {
			return fmt.Errorf("validate block size %d: must be positive", i)
		}
		if i == 0 {
			continue
		}
		previous := sizes[i-1]
		if size <= previous || size%previous != 0 || (size/previous)&(size/previous-1) != 0 {
			return fmt.Errorf("validate block size %d: %d is not a power-of-two multiple of the previous size %d", i, size, previous)
		}
	}
	return nil
}
