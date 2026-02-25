package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
