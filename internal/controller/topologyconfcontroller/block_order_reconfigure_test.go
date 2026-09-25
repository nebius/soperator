package topologyconfcontroller

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlockOrderFingerprintStaysBounded(t *testing.T) {
	var blocks []blockYAML
	for i := range 10000 {
		blocks = append(blocks, blockYAML{Block: fmt.Sprintf("rack-%059d", i), Nodes: fmt.Sprintf("worker-%d", i)})
	}
	rendered, err := renderTopologyYAML([]topologyYAMLEntry{{Topology: "blocks", Block: &blockTopologyYAML{Blocks: blocks}}})
	require.NoError(t, err)
	structure, err := topologyStructure(rendered)
	require.NoError(t, err)
	assert.Less(t, len(structure), 128)
}
