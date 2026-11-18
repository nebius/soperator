package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

func TestCheckStaticPowerState(t *testing.T) {
	tests := []struct {
		name        string
		activeNodes []int32
		found       bool
		wantErr     bool
	}{
		{name: "missing power state of an older operator", found: false},
		{name: "all ordinals active", activeNodes: []int32{0, 1, 2}, found: true},
		{name: "some ordinals inactive", activeNodes: []int32{0, 2}, found: true, wantErr: true},
		{name: "no ordinals active", found: true, wantErr: true},
		{name: "ordinal out of replicas", activeNodes: []int32{0, 1, 2, 3}, found: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			powerState := kubeobjects.NodeSetPowerState{
				Spec: kubeobjects.NodeSetPowerStateSpec{ActiveNodes: tt.activeNodes},
			}
			err := checkStaticPowerState(powerState, tt.found, allNodeSetOrdinals(3))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAllNodeSetOrdinals(t *testing.T) {
	assert.Empty(t, allNodeSetOrdinals(0))
	assert.Equal(t, []int32{0, 1, 2}, allNodeSetOrdinals(3))
}
