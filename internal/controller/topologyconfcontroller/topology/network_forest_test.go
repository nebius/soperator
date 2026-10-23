package topology

import (
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_networkVertex_isLeaf_Basic(t *testing.T) {
	assert.True(t, leafVertex("t0-3f9c0a", 0).isLeaf())
	assert.True(t, unknownVertex("gpucluster-5e8a1f").isLeaf())
	assert.False(t, innerVertex("t1-b71e4d", 1, leafVertex("t0-3f9c0a", 0)).isLeaf())
	assert.False(t, rootVertex("gpucluster-5e8a1f", unknownVertex("gpucluster-5e8a1f")).isLeaf())
}

func Test_networkVertex_traverseLeaves_ForestOrder(t *testing.T) {
	root := rootVertex("gpucluster-5e8a1f",
		innerVertex("t2-a0c2e8", 2,
			innerVertex("t1-b71e4d", 1, leafVertex("t0-3f9c0a", 0), leafVertex("t0-8d2b17", 0)),
			leafVertex("t0-c4e9d1", 0),
		),
		unknownVertex("gpucluster-5e8a1f"),
	)
	var visited []string
	root.traverseLeaves(func(leaf *networkVertex) { visited = append(visited, leaf.name) })
	assert.Equal(t, []string{"t0-3f9c0a", "t0-8d2b17", "t0-c4e9d1", "gpucluster-5e8a1f.unknown"}, visited)
}

func Test_networkVertex_traverseLeaves_LeafItself(t *testing.T) {
	var visited []string
	leafVertex("t0-3f9c0a", 0).traverseLeaves(func(leaf *networkVertex) { visited = append(visited, leaf.name) })
	assert.Equal(t, []string{"t0-3f9c0a"}, visited)
}

func Test_networkVertex_nodeNames_Basic(t *testing.T) {
	v := leafVertex("t0-3f9c0a", 0, testNode("worker-3", "worker", "root"), testNode("worker-1", "worker", "root"))
	assert.Equal(t, []string{"worker-3", "worker-1"}, v.nodeNames(), "names keep the order the nodes were attached in")
	assert.Empty(t, leafVertex("t0-3f9c0a", 0).nodeNames())
}

func Test_compareSiblings_Basic(t *testing.T) {
	spine := innerVertex("t2-a0c2e8", 2)
	leaf := innerVertex("t1-b71e4d", 1)
	rackA := leafVertex("t0-3f9c0a", 0)
	rackB := leafVertex("t0-8d2b17", 0)
	unknown := unknownVertex("root")
	tests := []struct {
		name string
		a, b *networkVertex
		want int
	}{
		{name: "a higher tier comes first", a: spine, b: leaf, want: -1},
		{name: "a lower tier comes last", a: leaf, b: spine, want: 1},
		{name: "equal tiers compare names", a: rackA, b: rackB, want: -1},
		{name: "equal tiers compare names, reversed", a: rackB, b: rackA, want: 1},
		{name: "a vertex equals itself", a: rackA, b: rackA, want: 0},
		{name: "unknown comes after a leaf of its own tier", a: rackA, b: unknown, want: -1},
		{name: "unknown comes after a higher tier", a: spine, b: unknown, want: -1},
		{name: "unknown comes last", a: unknown, b: rackA, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, compareSiblings(tt.a, tt.b))
		})
	}
}

func Test_sortChildren_Basic(t *testing.T) {
	children := []*networkVertex{
		unknownVertex("root"),
		leafVertex("t0-8d2b17", 0),
		innerVertex("t2-a0c2e8", 2),
		leafVertex("t0-3f9c0a", 0),
		innerVertex("t1-b71e4d", 1),
	}
	sortChildren(children)
	var names []string
	for _, child := range children {
		names = append(names, child.name)
	}
	assert.Equal(t, []string{"t2-a0c2e8", "t1-b71e4d", "t0-3f9c0a", "t0-8d2b17", "unknown"}, names)
}

func Test_forestBuilder_add_LowestSegment(t *testing.T) {
	b := newForestBuilder()
	n := node{name: "worker-0", nodeSet: "worker", fabric: "gpucluster-5e8a1f", path: segments("t0-3f9c0a", "t1-b71e4d", "t2-a0c2e8")}
	require.NoError(t, b.add(n))

	assert.Equal(t, []node{n}, b.vertices["t0-3f9c0a"].nodes)
	assert.Empty(t, b.vertices["t1-b71e4d"].nodes)
	assert.Empty(t, b.vertices["t2-a0c2e8"].nodes)
	assert.Equal(t, uint(1), b.vertices["t1-b71e4d"].tier)
	assert.Equal(t, map[string]NetworkPathSegment{
		"t0-3f9c0a": {Tier: 1, Name: "t1-b71e4d"},
		"t1-b71e4d": {Tier: 2, Name: "t2-a0c2e8"},
	}, b.parentOf)
	assert.Equal(t, map[string]bool{"gpucluster-5e8a1f": true}, b.fabrics)
	assert.Empty(t, b.unknownNodes)
}

func Test_forestBuilder_add_NoPath(t *testing.T) {
	b := newForestBuilder()
	down := testNode("worker-7", "worker", "gpucluster-5e8a1f")
	cpu := testNode("cpu-0", "cpu", "root")
	require.NoError(t, b.add(down))
	require.NoError(t, b.add(cpu))

	assert.Equal(t, map[string][]node{"gpucluster-5e8a1f": {down}, "root": {cpu}}, b.unknownNodes)
	assert.Empty(t, b.vertices)
	assert.Equal(t, map[string]bool{"gpucluster-5e8a1f": true, "root": true}, b.fabrics)
}

func Test_forestBuilder_add_PathsOfDifferentDepths(t *testing.T) {
	for name, paths := range map[string][][]NetworkPathSegment{
		"the deeper path first":    {segments("t0-e4a71b", "t1-c90d3f", "t2-71f2ab"), segments("t0-e4a71b", "t1-c90d3f")},
		"the shallower path first": {segments("t0-e4a71b", "t1-c90d3f"), segments("t0-e4a71b", "t1-c90d3f", "t2-71f2ab")},
	} {
		t.Run(name, func(t *testing.T) {
			b := newForestBuilder()
			for i, p := range paths {
				n := node{name: fmt.Sprintf("worker-rack0-%d", i), nodeSet: "worker-rack0", fabric: "gpucluster-9b4d2c", path: p}
				require.NoError(t, b.add(n))
			}
			assert.Equal(t, NetworkPathSegment{Tier: 1, Name: "t1-c90d3f"}, b.parentOf["t0-e4a71b"])
			assert.Equal(t, NetworkPathSegment{Tier: 2, Name: "t2-71f2ab"}, b.parentOf["t1-c90d3f"])
		})
	}
}

func Test_forestBuilder_add_TwoParents(t *testing.T) {
	b := newForestBuilder()
	require.NoError(t, b.add(node{name: "worker-rack0-0", path: segments("t0-e4a71b", "t1-c90d3f")}))
	err := b.add(node{name: "worker-rack0-1", path: segments("t0-e4a71b", "t1-5a8e02")})
	assert.EqualError(t, err, `add node "worker-rack0-1": segment "t0-e4a71b" has two parents, "t1-c90d3f" and "t1-5a8e02"`)
}

func Test_forestBuilder_add_TierChange(t *testing.T) {
	b := newForestBuilder()
	require.NoError(t, b.add(node{name: "worker-0", path: segments("t0-3f9c0a", "t1-b71e4d", "t2-a0c2e8")}))
	err := b.add(node{name: "worker-1", path: segments("t0-8d2b17", "t2-a0c2e8")})
	assert.EqualError(t, err, `add node "worker-1": segment "t2-a0c2e8" is at tier 2 in one path and tier 1 in another`)
}

func Test_forestBuilder_insertVertex_Basic(t *testing.T) {
	b := newForestBuilder()
	require.NoError(t, b.insertVertex(NetworkPathSegment{Tier: 1, Name: "t1-b71e4d"}))
	require.NoError(t, b.insertVertex(NetworkPathSegment{Tier: 1, Name: "t1-b71e4d"}), "a known segment at its tier is accepted again")
	assert.EqualError(t, b.insertVertex(NetworkPathSegment{Tier: 2, Name: "t1-b71e4d"}),
		`segment "t1-b71e4d" is at tier 1 in one path and tier 2 in another`)
	assert.Len(t, b.vertices, 1)
	assert.Equal(t, uint(1), b.vertices["t1-b71e4d"].tier)
}

func Test_forestBuilder_build_NodesOnInnerSegment(t *testing.T) {
	b := newForestBuilder()
	require.NoError(t, b.add(node{name: "worker-0", fabric: "root", path: segments("t0-3f9c0a", "t1-b71e4d")}))
	require.NoError(t, b.add(node{name: "worker-1", fabric: "root", path: segments("", "t1-b71e4d", "t2-a0c2e8")}))
	_, err := b.build()
	assert.EqualError(t, err, `segment "t1-b71e4d" holds node "worker-1" and is above segment "t0-3f9c0a"`)
}

func Test_newNetworkForest_FabricRoots(t *testing.T) {
	forest, err := newNetworkForest([]NodeSet{
		mustNodeSet("worker", 3, "gpucluster-5e8a1f", rackPaths("worker", 3, path("t0-3f9c0a", "t1-b71e4d", "t2-a0c2e8"), 2)),
		mustNodeSet("cpu", 1, "", nil),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"gpucluster-5e8a1f/t2-a0c2e8/t1-b71e4d/t0-3f9c0a=worker-[0-1]",
		"gpucluster-5e8a1f/gpucluster-5e8a1f.unknown=worker-2",
		"root/unknown=cpu-0",
	}, leafPaths(forest))
}

func Test_newNetworkForest_ChildOrder(t *testing.T) {
	// Under the root, a path ending at a core (tier-3) comes before one ending at a spine (tier-2)
	// whatever their names; racks under one leaf go by name; unknown is last.
	forest, err := newNetworkForest([]NodeSet{
		mustNodeSet("worker", 4, "", map[string]NetworkPath{
			"worker-0": path("t0-8d2b17", "t1-b71e4d", "t2-a0c2e8"),
			"worker-1": path("t0-3f9c0a", "t1-b71e4d", "t2-a0c2e8"),
			"worker-2": path("t0-0a1b2c", "t1-c4e9d1", "t2-e2d6c5", "t3-7b2e4c"),
		}),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"root/t3-7b2e4c/t2-e2d6c5/t1-c4e9d1/t0-0a1b2c=worker-2",
		"root/t2-a0c2e8/t1-b71e4d/t0-3f9c0a=worker-1",
		"root/t2-a0c2e8/t1-b71e4d/t0-8d2b17=worker-0",
		"root/unknown=worker-3",
	}, leafPaths(forest))
}

func Test_newNetworkForest_RootOrder(t *testing.T) {
	forest, err := newNetworkForest([]NodeSet{
		mustNodeSet("worker", 1, "gpucluster-2a9e", rackPaths("worker", 1, path("t0-77c1e0"))),
		mustNodeSet("worker-rack0", 1, "gpucluster-1f0c", rackPaths("worker-rack0", 1, path("t0-a3f0b9"))),
		mustNodeSet("cpu", 1, "", nil),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"gpucluster-1f0c/t0-a3f0b9=worker-rack0-0",
		"gpucluster-1f0c/gpucluster-1f0c.unknown=",
		"gpucluster-2a9e/t0-77c1e0=worker-0",
		"gpucluster-2a9e/gpucluster-2a9e.unknown=",
		"root/unknown=cpu-0",
	}, leafPaths(forest))
}

func Test_newNetworkForest_SharedTopSegment(t *testing.T) {
	forest, err := newNetworkForest([]NodeSet{
		mustNodeSet("worker", 1, "gpucluster-2a9e", rackPaths("worker", 1, path("t0-77c1e0", "t2-e2d6c5"))),
		mustNodeSet("worker-rack0", 1, "gpucluster-1f0c", rackPaths("worker-rack0", 1, path("t0-a3f0b9", "t2-e2d6c5"))),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"gpucluster-1f0c/t2-e2d6c5/t0-77c1e0=worker-0",
		"gpucluster-1f0c/t2-e2d6c5/t0-a3f0b9=worker-rack0-0",
		"gpucluster-1f0c/gpucluster-1f0c.unknown=",
		"gpucluster-2a9e/gpucluster-2a9e.unknown=",
	}, leafPaths(forest))
}

func Test_newNetworkForest_PathCompletion(t *testing.T) {
	// worker-rack0-1 knows only its rack and leaf; the leaf's spine comes from worker-rack0-0.
	forest, err := newNetworkForest([]NodeSet{
		mustNodeSet("worker-rack0", 2, "gpucluster-9b4d2c", map[string]NetworkPath{
			"worker-rack0-0": path("t0-e4a71b", "t1-c90d3f", "t2-71f2ab"),
			"worker-rack0-1": path("t0-e4a71b", "t1-c90d3f"),
		}),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"gpucluster-9b4d2c/t2-71f2ab/t1-c90d3f/t0-e4a71b=worker-rack0-[0-1]",
		"gpucluster-9b4d2c/gpucluster-9b4d2c.unknown=",
	}, leafPaths(forest))
}

func Test_newNetworkForest_NoNodes(t *testing.T) {
	forest, err := newNetworkForest(nil)
	require.NoError(t, err)
	assert.Empty(t, forest.roots)

	forest, err = newNetworkForest([]NodeSet{mustNodeSet("worker-rack0", 0, "gpucluster-9b4d2c", nil)})
	require.NoError(t, err)
	assert.Empty(t, forest.roots, "a NodeSet scaled to zero brings no fabric")
}

func Test_newNetworkForest_Contradiction(t *testing.T) {
	_, err := newNetworkForest([]NodeSet{
		mustNodeSet("worker-rack0", 2, "gpucluster-9b4d2c", map[string]NetworkPath{
			"worker-rack0-0": path("t0-e4a71b", "t1-c90d3f"),
			"worker-rack0-1": path("t0-e4a71b", "t1-5a8e02"),
		}),
	})
	assert.EqualError(t, err, `add node "worker-rack0-1": segment "t0-e4a71b" has two parents, "t1-c90d3f" and "t1-5a8e02"`)
}

// forestToCutOut is a cluster with an H100 NodeSet over two NVLink blocks on one fabric, a GB300
// rack on another, and CPU-only nodes on the default fabric. worker-4 is down.
func forestToCutOut(t *testing.T) networkForest {
	t.Helper()
	paths := nodePaths("worker", 0, 1, path("t0-3f9c0a", "t1-b71e4d", "t2-a0c2e8"))
	maps.Copy(paths, nodePaths("worker", 2, 3, path("t0-8d2b17", "t1-b71e4d", "t2-a0c2e8")))
	forest, err := newNetworkForest([]NodeSet{
		mustNodeSet("worker", 5, "gpucluster-5e8a1f", paths),
		mustNodeSet("worker-rack0", 2, "gpucluster-9b4d2c", rackPaths("worker-rack0", 2, path("t0-e4a71b", "t1-c90d3f", "t2-71f2ab"))),
		mustNodeSet("cpu", 2, "", nil),
	})
	require.NoError(t, err)
	return forest
}

var scopingForestLeaves = []string{
	"gpucluster-5e8a1f/t2-a0c2e8/t1-b71e4d/t0-3f9c0a=worker-[0-1]",
	"gpucluster-5e8a1f/t2-a0c2e8/t1-b71e4d/t0-8d2b17=worker-[2-3]",
	"gpucluster-5e8a1f/gpucluster-5e8a1f.unknown=worker-4",
	"gpucluster-9b4d2c/t2-71f2ab/t1-c90d3f/t0-e4a71b=worker-rack0-[0-1]",
	"gpucluster-9b4d2c/gpucluster-9b4d2c.unknown=",
	"root/unknown=cpu-[0-1]",
}

func Test_networkForest_cutOutNodeSets_AllNodeSets(t *testing.T) {
	forest := forestToCutOut(t)
	require.Equal(t, scopingForestLeaves, leafPaths(forest))
	assert.Equal(t, scopingForestLeaves, leafPaths(forest.cutOutNodeSets([]string{"worker", "worker-rack0", "cpu"})))
}

func Test_networkForest_cutOutNodeSets_OneNodeSet(t *testing.T) {
	scoped := forestToCutOut(t).cutOutNodeSets([]string{"worker-rack0"})
	assert.Equal(t, []string{
		"gpucluster-9b4d2c/t2-71f2ab/t1-c90d3f/t0-e4a71b=worker-rack0-[0-1]",
		"gpucluster-9b4d2c/gpucluster-9b4d2c.unknown=",
	}, leafPaths(scoped))
}

func Test_networkForest_cutOutNodeSets_UnknownVertex(t *testing.T) {
	scoped := forestToCutOut(t).cutOutNodeSets([]string{"worker"})
	assert.Equal(t, []string{
		"gpucluster-5e8a1f/t2-a0c2e8/t1-b71e4d/t0-3f9c0a=worker-[0-1]",
		"gpucluster-5e8a1f/t2-a0c2e8/t1-b71e4d/t0-8d2b17=worker-[2-3]",
		"gpucluster-5e8a1f/gpucluster-5e8a1f.unknown=worker-4",
	}, leafPaths(scoped))
}

func Test_networkForest_cutOutNodeSets_OtherFabrics(t *testing.T) {
	scoped := forestToCutOut(t).cutOutNodeSets([]string{"cpu"})
	assert.Equal(t, []string{"root/unknown=cpu-[0-1]"}, leafPaths(scoped))
}

func Test_networkForest_cutOutNodeSets_PrunesBranches(t *testing.T) {
	// Two racks share a leaf switch: scoping to one rack keeps the switches above it and drops
	// the other rack.
	forest, err := newNetworkForest([]NodeSet{
		mustNodeSet("worker-rack0", 1, "gpucluster-9b4d2c", rackPaths("worker-rack0", 1, path("t0-e4a71b", "t1-c90d3f", "t2-71f2ab"))),
		mustNodeSet("worker-rack1", 1, "gpucluster-9b4d2c", rackPaths("worker-rack1", 1, path("t0-0b6e95", "t1-c90d3f", "t2-71f2ab"))),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"gpucluster-9b4d2c/t2-71f2ab/t1-c90d3f/t0-e4a71b=worker-rack0-0",
		"gpucluster-9b4d2c/gpucluster-9b4d2c.unknown=",
	}, leafPaths(forest.cutOutNodeSets([]string{"worker-rack0"})))
}

func Test_networkForest_cutOutNodeSets_UnknownNames(t *testing.T) {
	forest := forestToCutOut(t)
	assert.Empty(t, forest.cutOutNodeSets([]string{"login"}).roots)
	assert.Empty(t, forest.cutOutNodeSets(nil).roots)
}

func Test_networkForest_cutOutNodeSets_SharedTopSegment(t *testing.T) {
	// Fabrics are expected to be disjoint. When two share a top segment, the nodes of the fabric
	// with the larger name sit under the other fabric's root and must still be in scope.
	forest, err := newNetworkForest([]NodeSet{
		mustNodeSet("worker", 1, "gpucluster-2a9e", rackPaths("worker", 1, path("t0-77c1e0", "t2-e2d6c5"))),
		mustNodeSet("worker-rack0", 2, "gpucluster-1f0c", rackPaths("worker-rack0", 2, path("t0-a3f0b9", "t2-e2d6c5"), 1)),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"gpucluster-1f0c/t2-e2d6c5/t0-77c1e0=worker-0",
		"gpucluster-1f0c/gpucluster-1f0c.unknown=",
		"gpucluster-2a9e/gpucluster-2a9e.unknown=",
	}, leafPaths(forest.cutOutNodeSets([]string{"worker"})))
}

func Test_networkForest_cutOutNodeSets_Immutable(t *testing.T) {
	forest := forestToCutOut(t)
	_ = forest.cutOutNodeSets([]string{"cpu"})
	assert.Equal(t, scopingForestLeaves, leafPaths(forest))
}
