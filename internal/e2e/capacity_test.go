package e2e

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGPUDemands_MixedProfile(t *testing.T) {
	p := mixedGPUProfile()
	demands, skipped := gpuDemands(p)
	assert.Equal(t, map[affinityKey]affinityDemand{
		{Platform: "gpu-h100-sxm", Fabric: "fabric-2"}: {requiredGPUs: 16, nodesetNames: []string{"worker-h100"}},
		{Platform: "gpu-h200-sxm", Fabric: "fabric-7"}: {requiredGPUs: 16, nodesetNames: []string{"worker-h200"}},
	}, demands)
	require.Len(t, skipped, 1)
	assert.Equal(t, "worker-cpu", skipped[0].name)
}

func TestGPUDemands_SharedAffinityAndPreemptibleWorkers(t *testing.T) {
	p := mixedGPUProfile()
	extra := p.NodeSets[0]
	extra.Name = "extra-h100"
	extra.Size = 1
	p.NodeSets = append(p.NodeSets, extra)
	extra.Name = "preemptible-h100"
	extra.Preemptible = true
	p.NodeSets = append(p.NodeSets, extra)
	demands, skipped := gpuDemands(p)
	assert.Equal(t, affinityDemand{requiredGPUs: 24, nodesetNames: []string{"worker-h100", "extra-h100"}},
		demands[affinityKey{Platform: "gpu-h100-sxm", Fabric: "fabric-2"}])
	assert.Len(t, demands, 2)
	assert.Len(t, skipped, 2)
}

func TestCheckCapacity_MixedProfile(t *testing.T) {
	for _, tc := range []struct {
		name        string
		h100        uint64
		h200        uint64
		missingH200 bool
		wantErr     error
	}{
		{name: "both fit exactly", h100: 16, h200: 16},
		{name: "h100 insufficient", h100: 15, h200: 32, wantErr: ErrInsufficientCapacity},
		{name: "h200 insufficient", h100: 32, h200: 15, wantErr: ErrInsufficientCapacity},
		{name: "both insufficient", h100: 15, h200: 15, wantErr: ErrInsufficientCapacity},
		{name: "h200 pool absent", h100: 16, missingH200: true, wantErr: ErrInsufficientCapacity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := mixedGPUProfile()
			demands, _ := gpuDemands(p)
			queried := make(map[affinityKey]bool)
			err := checkCapacity(p, demands, func(key affinityKey) (cbgAvailability, error) {
				queried[key] = true
				switch key {
				case affinityKey{Platform: "gpu-h100-sxm", Fabric: "fabric-2"}:
					return cbgAvailability{available: tc.h100}, nil
				case affinityKey{Platform: "gpu-h200-sxm", Fabric: "fabric-7"}:
					if tc.missingH200 {
						return cbgAvailability{}, errNoCapacityBlockGroup
					}
					return cbgAvailability{available: tc.h200}, nil
				default:
					t.Fatalf("unexpected capacity affinity: %+v", key)
					return cbgAvailability{}, nil
				}
			})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Len(t, queried, 2)
		})
	}
}

func TestCheckCapacity_APIError(t *testing.T) {
	p := mixedGPUProfile()
	demands, _ := gpuDemands(p)
	apiErr := errors.New("capacity API unavailable")
	err := checkCapacity(p, demands, func(affinityKey) (cbgAvailability, error) {
		return cbgAvailability{}, apiErr
	})
	require.ErrorIs(t, err, apiErr)
}

func TestCheckCapacity_WarnAllowsInsufficientCapacity(t *testing.T) {
	p := mixedGPUProfile()
	p.CapacityStrategy = CapacityStrategyWarn
	demands, _ := gpuDemands(p)
	err := checkCapacity(p, demands, func(affinityKey) (cbgAvailability, error) {
		return cbgAvailability{}, nil
	})
	require.NoError(t, err)
}

func TestCheckCapacity_MissingTokenSkipsAllStrategies(t *testing.T) {
	t.Setenv("NEBIUS_IAM_TOKEN", "")
	for _, strategy := range []CapacityStrategy{CapacityStrategyWarn, CapacityStrategyCancel} {
		t.Run(string(strategy), func(t *testing.T) {
			p := mixedGPUProfile()
			p.CapacityStrategy = strategy
			require.NoError(t, CheckCapacity(t.Context(), p))
		})
	}
}

func TestParseGPUCount(t *testing.T) {
	tests := []struct {
		preset string
		want   int
	}{
		{"8gpu-128vcpu-1600gb", 8},
		{"1gpu-16vcpu-200gb", 1},
		{"16gpu-256vcpu-3200gb", 16},
		{"16vcpu-64gb", 0},
		{"cpu-only-preset", 0},
		{"", 0},
		{"0gpu-128vcpu-1600gb", 0},
	}
	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			assert.Equal(t, tt.want, parseGPUCount(tt.preset))
		})
	}
}
