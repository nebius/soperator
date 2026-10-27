package topology

import (
	"fmt"
	"slices"
	"strings"
)

// NetworkPathSegment is one segment of a node's network path and its tier; tier-0 is closest to
// the node.
type NetworkPathSegment struct {
	Tier uint
	Name string
}

// NetworkPath is a node's path within its fabric.
type NetworkPath struct {
	segments []NetworkPathSegment
}

// NewNetworkPath creates and validates a new network path for a Slurm node. A name Slurm cannot read,
// a tier named twice or a tier skipped are not allowed.
func NewNetworkPath(segments ...NetworkPathSegment) (NetworkPath, error) {
	if len(segments) == 0 {
		return NetworkPath{}, nil
	}
	sorted := slices.Clone(segments)
	slices.SortFunc(sorted, func(a, b NetworkPathSegment) int {
		switch {
		case a.Tier < b.Tier:
			return -1
		case a.Tier > b.Tier:
			return 1
		}
		return 0
	})
	for i, segment := range sorted {
		if err := validateSegmentName(segment.Name); err != nil {
			return NetworkPath{}, fmt.Errorf("validate tier-%d segment %q: %w", segment.Tier, segment.Name, err)
		}
		if i > 0 && sorted[i-1].Tier == segment.Tier {
			return NetworkPath{}, fmt.Errorf("validate tier-%d: two segments at one tier", segment.Tier)
		}
		if i > 0 && sorted[i-1].Tier+1 != segment.Tier {
			return NetworkPath{}, fmt.Errorf("validate tier-%d segment %q: tier-%d below it is missing",
				segment.Tier, segment.Name, segment.Tier-1)
		}
	}
	return NetworkPath{segments: sorted}, nil
}

// Segments returns a copy of the path's segments, lowest tier first.
func (p NetworkPath) Segments() []NetworkPathSegment {
	return slices.Clone(p.segments)
}

// String formats the path as "tier-0=name/tier-1=name".
func (p NetworkPath) String() string {
	return formatSegments(p.segments)
}

func formatSegments(segments []NetworkPathSegment) string {
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		parts = append(parts, fmt.Sprintf("tier-%d=%s", segment.Tier, segment.Name))
	}
	return strings.Join(parts, "/")
}

// hostlistReservedChars have a meaning in Slurm's hostlist syntax: separators, range brackets and
// function braces.
const hostlistReservedChars = " \t\n,[]{}"

// validateSegmentName reports why a name cannot be a switch or block name. Slurm reads those names
// and the "Switches=" lists referencing them through its hostlist parser: a reserved character
// splits the name, so the child reference no longer matches and slurmctld dies with "has invalid
// child".
func validateSegmentName(name string) error {
	if name == "" {
		return fmt.Errorf("name is empty")
	}
	if i := strings.IndexAny(name, hostlistReservedChars); i >= 0 {
		return fmt.Errorf("name contains %q, which Slurm's hostlist syntax reserves", name[i])
	}
	return nil
}
