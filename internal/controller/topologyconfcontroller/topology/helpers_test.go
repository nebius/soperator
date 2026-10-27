package topology

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"nebius.ai/slurm-operator/internal/utils/slurm/nodelist"
)

// segments builds path segments from tier names in tier order; an empty name leaves that tier out.
func segments(names ...string) []NetworkPathSegment {
	var result []NetworkPathSegment
	for tier, name := range names {
		if name != "" {
			result = append(result, NetworkPathSegment{Tier: uint(tier), Name: name})
		}
	}
	return result
}

// path builds a NetworkPath from tier names, see segments. The names are expected to be valid.
func path(names ...string) NetworkPath {
	p, err := NewNetworkPath(segments(names...)...)
	if err != nil {
		panic(err)
	}
	return p
}

func mustSpec(name, kind string, clusterDefault bool, blockSizes []int, nodeSetNames []string) Spec {
	spec, err := NewSpec(name, kind, clusterDefault, blockSizes, nodeSetNames)
	if err != nil {
		panic(err)
	}
	return spec
}

func mustNodeSet(name string, size uint, fabric string, paths map[string]NetworkPath) NodeSet {
	nodeSet, err := NewNodeSet(name, size, fabric, paths)
	if err != nil {
		panic(err)
	}
	return nodeSet
}

// nodePaths gives the nodes first to last of a NodeSet, inclusive, the same path.
func nodePaths(nodeSet string, first, last uint, p NetworkPath) map[string]NetworkPath {
	paths := make(map[string]NetworkPath, last-first+1)
	for i := first; i <= last; i++ {
		paths[fmt.Sprintf("%s-%d", nodeSet, i)] = p
	}
	return paths
}

// rackPaths gives every node of a NodeSet the same path, except the down ones, which get none:
// the shape of a rack whose nodes share one position in the fabric.
func rackPaths(nodeSet string, size uint, p NetworkPath, down ...uint) map[string]NetworkPath {
	if size == 0 {
		return nil
	}
	paths := nodePaths(nodeSet, 0, size-1, p)
	for _, i := range down {
		delete(paths, fmt.Sprintf("%s-%d", nodeSet, i))
	}
	return paths
}

// preOrderSwitches lists every switch of a tree in the order it is written.
func preOrderSwitches(tree TreeTopology) []string {
	names := make([]string, 0, len(tree.Switches))
	for _, sw := range tree.Switches {
		names = append(names, sw.Name)
	}
	return names
}

func switchNamed(tree TreeTopology, name string) Switch {
	for _, sw := range tree.Switches {
		if sw.Name == name {
			return sw
		}
	}
	panic("no switch named " + name)
}

// preOrderLeaves lists the switches of a tree that hold nodes, in the order they are written.
func preOrderLeaves(tree TreeTopology) []string {
	var names []string
	for _, sw := range tree.Switches {
		if sw.Nodes != "" {
			names = append(names, sw.Name)
		}
	}
	return names
}

func topologyNames(topologies *MultiTopology) []string {
	names := make([]string, 0, len(*topologies))
	for _, topology := range *topologies {
		names = append(names, topology.Name)
	}
	return names
}

func byName(topologies *MultiTopology, name string) SlurmTopology {
	for _, topology := range *topologies {
		if topology.Name == name {
			return topology
		}
	}
	panic("no topology named " + name)
}

func blockNames(block BlockTopology) []string {
	names := make([]string, 0, len(block.Blocks))
	for _, b := range block.Blocks {
		names = append(names, b.Name)
	}
	return names
}

// restrictTo keeps the elements of sequence that are in keep, in sequence order.
func restrictTo(sequence, keep []string) []string {
	var out []string
	for _, name := range sequence {
		if slices.Contains(keep, name) {
			out = append(out, name)
		}
	}
	return out
}

func testNode(name, nodeSet, fabric string) node {
	return node{name: name, nodeSet: nodeSet, fabric: fabric}
}

func leafVertex(name string, tier uint, nodes ...node) *networkVertex {
	return &networkVertex{name: name, tier: tier, nodes: nodes}
}

func innerVertex(name string, tier uint, children ...*networkVertex) *networkVertex {
	return &networkVertex{name: name, tier: tier, children: children}
}

func unknownVertex(fabric string, nodes ...node) *networkVertex {
	return &networkVertex{name: unknownLeafName(fabric), unknown: true, nodes: nodes}
}

func rootVertex(fabric string, children ...*networkVertex) *networkVertex {
	return &networkVertex{name: fabric, children: children}
}

// leafPaths flattens a forest into one line per leaf in forest order: the vertex names from the
// root down to the leaf, then the leaf's nodes.
func leafPaths(f networkForest) []string {
	var lines []string
	var walk func(v *networkVertex, prefix string)
	walk = func(v *networkVertex, prefix string) {
		name := prefix + v.name
		if v.isLeaf() {
			lines = append(lines, fmt.Sprintf("%s=%s", name, nodelist.NewNodeList(v.nodeNames())))
			return
		}
		for _, child := range v.children {
			walk(child, name+"/")
		}
	}
	for _, root := range f.roots {
		walk(root, "")
	}
	return lines
}

// scenarioNodeSet is one NodeSet of a scenarioCluster.
type scenarioNodeSet struct {
	name   string
	fabric string
	size   uint
	paths  map[string]NetworkPath
}

// scenarioCluster is a cluster as the controller sees it at one reconciliation: CPU-only nodes in
// a flat topology, HGX NodeSets in the tree, NVL72 racks in the tree and in the block topology.
// The tree is the cluster default as soon as there is a GPU NodeSet.
type scenarioCluster struct {
	cpu        uint
	hgx        []scenarioNodeSet
	racks      []scenarioNodeSet
	blockSizes []int
}

func (c scenarioCluster) topologies(t *testing.T) *MultiTopology {
	t.Helper()
	nodeSets := []NodeSet{mustNodeSet("cpu", c.cpu, "", nil)}
	var gpu, racks []string
	for _, ns := range c.hgx {
		nodeSets = append(nodeSets, mustNodeSet(ns.name, ns.size, ns.fabric, ns.paths))
		gpu = append(gpu, ns.name)
	}
	for _, ns := range c.racks {
		nodeSets = append(nodeSets, mustNodeSet(ns.name, ns.size, ns.fabric, ns.paths))
		gpu = append(gpu, ns.name)
		racks = append(racks, ns.name)
	}

	specs := []Spec{mustSpec("flat", "flat", len(gpu) == 0, nil, []string{"cpu"})}
	if len(gpu) > 0 {
		specs = append(specs, mustSpec("tree", "tree", true, nil, gpu))
	}
	if len(racks) > 0 {
		sizes := c.blockSizes
		if sizes == nil {
			sizes = []int{18, 36}
		}
		specs = append(specs, mustSpec("block", "block", false, sizes, racks))
	}

	result, err := NewMultiTopology(specs, nodeSets)
	require.NoError(t, err)
	topologies := MultiTopology(*result)
	return &topologies
}

// hgxSwitches are the InfiniBand leaf switches, spines and core of an HGX fabric: sixteen nodes
// per leaf switch, two leaf switches per spine.
var hgxSwitches = map[string]struct {
	leaves []string
	spines []string
	core   string
}{
	"gpucluster-2a9e": {
		leaves: []string{"t0-77c1e0", "t0-b4d2f0", "t0-09ce3a", "t0-52ee71"},
		spines: []string{"t1-4b9f3a", "t1-4b9f3a", "t1-d7a1b3", "t1-d7a1b3"},
		core:   "t2-e2d6c5",
	},
	"gpucluster-3c3c": {
		leaves: []string{"t0-1a2b3c", "t0-4d5e6f", "t0-7a8b9c", "t0-0d1e2f"},
		spines: []string{"t1-3a4b5c", "t1-3a4b5c", "t1-6d7e8f", "t1-6d7e8f"},
		core:   "t2-9a0b1c",
	},
}

// hgxNodeSet places the nodes of an HGX NodeSet sixteen per leaf switch; the down ones get no path.
func hgxNodeSet(name, fabric string, size uint, down ...uint) scenarioNodeSet {
	sw := hgxSwitches[fabric]
	paths := make(map[string]NetworkPath, size)
	for i := range size {
		paths[fmt.Sprintf("%s-%d", name, i)] = path(sw.leaves[i/16], sw.spines[i/16], sw.core)
	}
	for _, i := range down {
		delete(paths, fmt.Sprintf("%s-%d", name, i))
	}
	return scenarioNodeSet{name: name, fabric: fabric, size: size, paths: paths}
}

// nvl72Switches are the block, leaf switch and spine of each NVL72 rack of the fabric
// "gpucluster-1f0c": racks 0 and 1 share a leaf, rack 2 has its own.
var nvl72Switches = []NetworkPath{
	path("t0-a3f0b9", "t1-1c7d4e", "t2-90be2f"),
	path("t0-f82c1a", "t1-1c7d4e", "t2-90be2f"),
	path("t0-d13c7e", "t1-5a8e02", "t2-90be2f"),
}

// nvl72Rack is the NodeSet "worker-rack<rack>" with every node on the rack's path.
func nvl72Rack(rack int, size uint) scenarioNodeSet {
	name := fmt.Sprintf("worker-rack%d", rack)
	return scenarioNodeSet{name: name, fabric: "gpucluster-1f0c", size: size, paths: rackPaths(name, size, nvl72Switches[rack])}
}

// withPath gives one node of the NodeSet the given path.
func (ns scenarioNodeSet) withPath(node string, p NetworkPath) scenarioNodeSet {
	paths := maps.Clone(ns.paths)
	paths[node] = p
	return scenarioNodeSet{name: ns.name, fabric: ns.fabric, size: ns.size, paths: paths}
}
