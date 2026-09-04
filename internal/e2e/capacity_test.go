package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseGPUCount(t *testing.T) {
	tests := []struct {
		preset string
		want   int
	}{
		{"8gpu-128vcpu-1600gb", 8},
		{"4gpu-112vcpu-800gb", 4},
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

func TestGPUDemands_GB300(t *testing.T) {
	profile := Profile{
		NodeSets: []NodeSetDef{
			{
				Name:             "worker",
				Platform:         "gpu-gb300",
				Preset:           "4gpu-112vcpu-800gb",
				Size:             2,
				InfinibandFabric: "gb300-fabric",
			},
		},
	}

	demands, skipped := gpuDemands(profile)
	assert.Empty(t, skipped)
	assert.Equal(t, map[affinityKey]affinityDemand{
		{Platform: "gpu-gb300", Fabric: "gb300-fabric"}: {
			requiredGPUs: 8,
			nodesetNames: []string{"worker"},
		},
	}, demands)
}

func TestCheckCapacity_CPUProfileDoesNotRequireCredentials(t *testing.T) {
	profile := Profile{
		NodeSets: []NodeSetDef{
			{Name: "worker", Platform: "cpu-d3", Preset: "16vcpu-64gb", Size: 1},
		},
	}

	assert.NoError(t, CheckCapacity(context.Background(), profile))
}
