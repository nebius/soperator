package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePartitionNodes(t *testing.T) {
	assert.Equal(t, "sinfo --noheader --Node --partition='main' --format='%N|%X|%Y|%T'", partitionNodesCommand("main"))

	nodes, err := parsePartitionNodes("worker-1|2|40|idle\nworker-0|2|40|mixed\nworker-cpu-0|1|4|idle\nworker-0|2|40|mixed\n")
	require.NoError(t, err)
	assert.Equal(t, []partitionNode{
		{Name: "worker-0", Cores: 80, State: "mixed"},
		{Name: "worker-1", Cores: 80, State: "idle"},
		{Name: "worker-cpu-0", Cores: 4, State: "idle"},
	}, nodes)

	nodes, err = parsePartitionNodes("")
	require.NoError(t, err)
	assert.Empty(t, nodes)
	_, err = parsePartitionNodes("worker-0|2|40")
	require.ErrorContains(t, err, "unexpected sinfo row")
	_, err = parsePartitionNodes("worker-0|two|40|idle")
	require.ErrorContains(t, err, "parse sockets of node worker-0")
}
