package topology

import (
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"nebius.ai/slurm-operator/internal/utils/slurm/nodelist"
)

// UnknownLeaf names a fabric's catch-all leaf switch for nodes without a path: "unknown" for the
// default fabric, "<fabric>.unknown" otherwise.
const UnknownLeaf = "unknown"

func unknownLeafName(fabric string) string {
	if fabric == DefaultFabric {
		return UnknownLeaf
	}
	return fabric + "." + UnknownLeaf
}

// TreeTopology represents Slurm topology with type "tree" in topology.yaml format
type TreeTopology struct {
	Switches []Switch `yaml:"switches"`
}

// Switch is one switch of a tree with its children or its nodes, in the order they are written.
type Switch struct {
	Name     string            `yaml:"switch"`
	Children SwitchList        `yaml:"children,omitempty"`
	Nodes    nodelist.NodeList `yaml:"nodes,omitempty"`
}

// treeTopologyPlaceholder is the minimal tree topology configuration that doesn't break slurmctld.
var treeTopologyPlaceholder = &TreeTopology{
	Switches: []Switch{{
		Name: UnknownLeaf,
	}},
}

// newTreeTopology builds the Slurm tree topology.
//
// It writes the scoped forest as a pre-order list of switches, skipping an unknown leaf with no
// nodes.
func newTreeTopology(forest networkForest) *TreeTopology {
	var switches []Switch
	var build func(v *networkVertex) bool
	build = func(v *networkVertex) bool {
		sw := Switch{Name: v.name}
		if v.isLeaf() {
			if len(v.nodes) == 0 {
				return false
			}
			sw.Nodes = nodelist.NewNodeList(v.nodeNames())
			switches = append(switches, sw)
			return true
		}
		at := len(switches)
		switches = append(switches, sw)
		for _, child := range v.children {
			if build(child) {
				switches[at].Children = append(switches[at].Children, child.name)
			}
		}
		if len(switches[at].Children) == 0 {
			switches = switches[:at]
			return false
		}
		return true
	}

	for _, root := range forest.roots {
		build(root)
	}
	if len(switches) == 0 {
		return treeTopologyPlaceholder
	}
	return &TreeTopology{Switches: switches}
}

// changeFrom compares the leaf switches in order, which node ranks come from: any difference there
// is ChangeReordered, a leaf appearing or disappearing included, since Slurm adds a registered switch at
// the end of its table and keeps a vanished one. Node lists, inner switches and the order inside a
// children list are ChangeRegistered.
func (t *TreeTopology) changeFrom(previous *TreeTopology) Change {
	if !slices.Equal(t.leaves(), previous.leaves()) {
		return ChangeReordered
	}
	if len(t.Switches) != len(previous.Switches) {
		return ChangeRegistered
	}
	for i, sw := range t.Switches {
		before := previous.Switches[i]
		sameChildrenOrder := slices.Equal(
			slices.Sorted(slices.Values(sw.Children)),
			slices.Sorted(slices.Values(before.Children)),
		)
		if sw.Name != before.Name || sw.Nodes != before.Nodes || !sameChildrenOrder {
			return ChangeRegistered
		}
	}
	return ChangeIdentical
}

func (t *TreeTopology) leaves() []string {
	var names []string
	for _, sw := range t.Switches {
		if sw.Nodes != "" {
			names = append(names, sw.Name)
		}
	}
	return names
}

// SwitchList if a list of switch names that is represented as a comma-separated string in YAML.
type SwitchList []string

func (sl SwitchList) MarshalYAML() (any, error) {
	sorted := slices.Clone(sl)
	slices.Sort(sorted)
	return strings.Join(sorted, ","), nil
}

func (sl *SwitchList) UnmarshalYAML(node *yaml.Node) error {
	var str string
	if err := node.Decode(&str); err != nil {
		return err
	}
	if str == "" {
		*sl = nil
		return nil
	}
	*sl = strings.Split(str, ",")
	return nil
}
