package topology

import (
	"fmt"
	"slices"
	"strings"
)

// networkForest is the cluster's network: a root per fabric, a vertex per path segment beneath it,
// and every node attached to its lowest known segment or to the fabric's unknown vertex. Children
// are ordered, so a pre-order walk is the order Slurm ranks nodes by regardless of the topology type.
type networkForest struct {
	roots []*networkVertex
}

// networkVertex is a fabric root, a path segment, or a fabric's unknown vertex holding the nodes
// without a path. Nodes sit on leaves only: a vertex has children or nodes, never both.
type networkVertex struct {
	name     string
	tier     uint
	unknown  bool
	children []*networkVertex
	nodes    []node
}

func (v *networkVertex) isLeaf() bool {
	return len(v.children) == 0
}

// traverseLeaves visits the leaves beneath the vertex in forest order.
func (v *networkVertex) traverseLeaves(visit func(leaf *networkVertex)) {
	if v.isLeaf() {
		visit(v)
		return
	}
	for _, child := range v.children {
		child.traverseLeaves(visit)
	}
}

func (v *networkVertex) nodeNames() []string {
	names := make([]string, 0, len(v.nodes))
	for _, n := range v.nodes {
		names = append(names, n.name)
	}
	return names
}

// newNetworkForest builds the forest from every node of every NodeSet. Vertices are identified by
// path segment name, as Slurm identifies switches. One name at two tiers, under two parents at
// one tier, or holding nodes while above another segment is an error.
func newNetworkForest(nodeSets []NodeSet) (networkForest, error) {
	b := newForestBuilder()
	for _, ns := range nodeSets {
		for _, n := range ns.nodes() {
			if err := b.add(n); err != nil {
				return networkForest{}, err
			}
		}
	}
	return b.build()
}

// cutOutNodeSets copies the forest keeping only the nodes of the named NodeSets, the vertices
// above them, and the roots of their fabrics with their unknown vertex, even empty. Order is
// unchanged; names of NodeSets that do not exist match nothing.
func (f networkForest) cutOutNodeSets(names []string) networkForest {
	inScope := func(n node) bool { return slices.Contains(names, n.nodeSet) }

	// Select fabrics (forest root vertices) containing nodes from the scope.
	fabrics := make(map[string]bool)
	for _, root := range f.roots {
		root.traverseLeaves(func(leaf *networkVertex) {
			for _, n := range leaf.nodes {
				if inScope(n) {
					fabrics[n.fabric] = true
				}
			}
		})
	}

	// A func to copy a subtree keeping only the nodes from the scope and the vertices above them.
	var restrict func(v *networkVertex) (*networkVertex, bool)
	restrict = func(v *networkVertex) (*networkVertex, bool) {
		kept := &networkVertex{name: v.name, tier: v.tier, unknown: v.unknown}
		if v.isLeaf() {
			for _, n := range v.nodes {
				if inScope(n) {
					kept.nodes = append(kept.nodes, n)
				}
			}
			return kept, len(kept.nodes) > 0
		}
		for _, child := range v.children {
			if c, ok := restrict(child); ok {
				kept.children = append(kept.children, c)
			}
		}
		return kept, len(kept.children) > 0
	}

	// Copy the selected fabrics and those holding scoped nodes under another fabric's root.
	var roots []*networkVertex
	for _, root := range f.roots {
		// Keep the root's unknown vertex, its last child, so an empty unknown block survives.
		kept, ok := restrict(root)
		if !ok && !fabrics[root.name] {
			continue
		}
		unknown := root.children[len(root.children)-1]
		if n := len(kept.children); n == 0 || !kept.children[n-1].unknown {
			kept.children = append(kept.children, &networkVertex{name: unknown.name, unknown: true})
		}
		roots = append(roots, kept)
	}
	return networkForest{roots: roots}
}

// ConflictingPathsError is the error for node network paths that contradict each other.
type ConflictingPathsError struct {
	msg string
}

func (e ConflictingPathsError) Error() string {
	return e.msg
}

type forestBuilder struct {
	vertices     map[string]*networkVertex
	parentOf     map[string]NetworkPathSegment
	unknownNodes map[string][]node
	fabrics      map[string]bool
}

func newForestBuilder() *forestBuilder {
	return &forestBuilder{
		vertices:     make(map[string]*networkVertex),
		parentOf:     make(map[string]NetworkPathSegment),
		unknownNodes: make(map[string][]node),
		fabrics:      make(map[string]bool),
	}
}

func (b *forestBuilder) add(n node) error {
	b.fabrics[n.fabric] = true
	if len(n.path) == 0 {
		b.unknownNodes[n.fabric] = append(b.unknownNodes[n.fabric], n)
		return nil
	}
	for i, segment := range n.path {
		if err := b.insertVertex(segment); err != nil {
			return fmt.Errorf("add node %q: %w", n.name, err)
		}
		if i == 0 {
			continue
		}
		// Paths of different depths may both name a segment's parent; they must agree.
		child, parent := n.path[i-1], segment
		if known, ok := b.parentOf[child.Name]; ok && known.Name != parent.Name {
			return ConflictingPathsError{
				msg: fmt.Sprintf("add node %q: segment %q has two parents, %q and %q", n.name, child.Name, known.Name, parent.Name),
			}
		}
		b.parentOf[child.Name] = parent
	}
	lowest := b.vertices[n.path[0].Name]
	lowest.nodes = append(lowest.nodes, n)
	return nil
}

func (b *forestBuilder) insertVertex(segment NetworkPathSegment) error {
	v, ok := b.vertices[segment.Name]
	if !ok {
		b.vertices[segment.Name] = &networkVertex{name: segment.Name, tier: segment.Tier}
		return nil
	}
	if v.tier != segment.Tier {
		return ConflictingPathsError{
			msg: fmt.Sprintf("segment %q is at tier %d in one path and tier %d in another", segment.Name, v.tier, segment.Tier),
		}
	}
	return nil
}

func (b *forestBuilder) build() (networkForest, error) {
	for child, parent := range b.parentOf {
		if nodes := b.vertices[parent.Name].nodes; len(nodes) > 0 {
			return networkForest{}, ConflictingPathsError{
				msg: fmt.Sprintf("segment %q holds node %q and is above segment %q", parent.Name, nodes[0].name, child),
			}
		}
	}

	// No path names a fabric root: a top segment hangs on the root of the fabric of the nodes
	// beneath it, and nodes of two fabrics beneath one segment contradict each other. Parents are
	// always at a higher tier, so the climb ends.
	fabricOfRoot := make(map[string]string)
	for name, s := range b.vertices {
		root := name
		for parent, ok := b.parentOf[root]; ok; parent, ok = b.parentOf[root] {
			root = parent.Name
		}
		for _, n := range s.nodes {
			if current, ok := fabricOfRoot[root]; ok && current != n.fabric {
				return networkForest{}, ConflictingPathsError{
					msg: fmt.Sprintf("segment %q is shared by fabrics %q and %q", root, min(current, n.fabric), max(current, n.fabric)),
				}
			}
			fabricOfRoot[root] = n.fabric
		}
	}

	rootsByFabric := make(map[string]*networkVertex, len(b.fabrics))
	for fabric := range b.fabrics {
		rootsByFabric[fabric] = &networkVertex{name: fabric}
	}
	for name, s := range b.vertices {
		if parent, ok := b.parentOf[name]; ok {
			b.vertices[parent.Name].children = append(b.vertices[parent.Name].children, s)
			continue
		}
		rootsByFabric[fabricOfRoot[name]].children = append(rootsByFabric[fabricOfRoot[name]].children, s)
	}
	for fabric, root := range rootsByFabric {
		root.children = append(root.children, &networkVertex{
			name:    unknownLeafName(fabric),
			unknown: true,
			nodes:   b.unknownNodes[fabric],
		})
	}

	for _, s := range b.vertices {
		sortChildren(s.children)
	}
	roots := make([]*networkVertex, 0, len(rootsByFabric))
	for _, root := range rootsByFabric {
		sortChildren(root.children)
		roots = append(roots, root)
	}
	slices.SortFunc(roots, func(a, b *networkVertex) int { return strings.Compare(a.name, b.name) })
	return networkForest{roots: roots}, nil
}

func sortChildren(children []*networkVertex) {
	slices.SortFunc(children, compareSiblings)
}

// compareSiblings orders first by the unknown flag, then tier (descending), then name.
func compareSiblings(a, b *networkVertex) int {
	if a.unknown != b.unknown {
		if a.unknown {
			return 1
		}
		return -1
	}
	if a.tier != b.tier {
		if a.tier > b.tier {
			return -1
		}
		return 1
	}
	return strings.Compare(a.name, b.name)
}
