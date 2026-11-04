package topology

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_NewNodeSet_DefaultFabric(t *testing.T) {
	ns, err := NewNodeSet("cpu", 2, "", nil)
	require.NoError(t, err)
	assert.Equal(t, DefaultFabric, ns.fabric)
}

func Test_NewNodeSet_NamedFabric(t *testing.T) {
	ns, err := NewNodeSet("worker", 8, "gpucluster-5e8a1f", nil)
	require.NoError(t, err)
	assert.Equal(t, "gpucluster-5e8a1f", ns.fabric)
}

func Test_NewNodeSet_OwnNodePaths(t *testing.T) {
	paths := map[string]NetworkPath{"worker-0": path("t0-3f9c0a"), "worker-7": path("t0-8d2b17")}
	ns, err := NewNodeSet("worker", 8, "", paths)
	require.NoError(t, err)
	assert.Equal(t, paths, ns.nodeNetworkPaths)
}

func Test_NewNodeSet_ForeignNodePath(t *testing.T) {
	for _, name := range []string{"worker-8", "login-0", "worker-01", "worker"} {
		_, err := NewNodeSet("worker", 8, "", map[string]NetworkPath{name: path("t0-3f9c0a")})
		assert.EqualError(t, err,
			fmt.Sprintf(`validate NodeSet "worker": node %q of its network paths is not one of its nodes`, name), name)
	}
}

func Test_NodeSet_Name_Basic(t *testing.T) {
	assert.Equal(t, "worker-rack0", mustNodeSet("worker-rack0", 18, "", nil).Name())
}

func Test_NodeSet_hasNode_Basic(t *testing.T) {
	ns := mustNodeSet("worker", 12, "", nil)
	for _, name := range []string{"worker-0", "worker-5", "worker-11"} {
		assert.True(t, ns.hasNode(name), name)
	}
	for _, name := range []string{
		"worker-12",
		"worker",
		"worker-",
		"worker-01",
		"worker-+1",
		"worker--1",
		"worker-a",
		"worker-0-1",
		"worker-18446744073709551616",
		"cpu-0",
		"workers-0",
		"worker-rack0-0",
	} {
		assert.False(t, ns.hasNode(name), name)
	}
}

func Test_NodeSet_hasNode_EmptyNodeSet(t *testing.T) {
	assert.False(t, mustNodeSet("worker", 0, "", nil).hasNode("worker-0"))
}

func Test_NodeSet_nodes_IndexOrder(t *testing.T) {
	ns := mustNodeSet("worker-rack0", 3, "gpucluster-9b4d2c", map[string]NetworkPath{
		"worker-rack0-1": path("t0-e4a71b", "t1-c90d3f"),
	})
	assert.Equal(t, []node{
		{name: "worker-rack0-0", nodeSet: "worker-rack0", fabric: "gpucluster-9b4d2c"},
		{name: "worker-rack0-1", nodeSet: "worker-rack0", fabric: "gpucluster-9b4d2c", path: segments("t0-e4a71b", "t1-c90d3f")},
		{name: "worker-rack0-2", nodeSet: "worker-rack0", fabric: "gpucluster-9b4d2c"},
	}, ns.nodes())
}

func Test_NodeSet_nodes_EmptyNodeSet(t *testing.T) {
	assert.Empty(t, mustNodeSet("worker", 0, "", nil).nodes())
}
