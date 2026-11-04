package topology

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_newFlatTopology_Basic(t *testing.T) {
	assert.Equal(t, FlatTopology(true), *newFlatTopology())
}

func Test_FlatTopology_changeFrom_Basic(t *testing.T) {
	enabled, disabled := FlatTopology(true), FlatTopology(false)
	assert.Equal(t, ChangeIdentical, enabled.changeFrom(&enabled))
	assert.Equal(t, ChangeIdentical, disabled.changeFrom(&disabled))
	assert.Equal(t, ChangeRestructured, enabled.changeFrom(&disabled))
	assert.Equal(t, ChangeRestructured, disabled.changeFrom(&enabled))
}
