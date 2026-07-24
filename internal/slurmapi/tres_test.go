package slurmapi

import (
	"testing"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func TestParseTrackableResources_Success(t *testing.T) {
	tests := []struct {
		input    string
		expected TrackableResources
	}{
		{
			input:    "cpu=16,mem=191356M,billing=16,gres/gpu=1",
			expected: TrackableResources{CPUCount: 16, MemoryBytes: 191356 * 1024 * 1024, GPUCount: 1},
		},
		{
			input:    "cpu=4,mem=8G,gres/gpu=2",
			expected: TrackableResources{CPUCount: 4, MemoryBytes: 8 * 1024 * 1024 * 1024, GPUCount: 2},
		},
		{
			input:    "cpu=2,mem=1024K",
			expected: TrackableResources{CPUCount: 2, MemoryBytes: 1024 * 1024, GPUCount: 0},
		},
		{
			input:    "cpu=1,mem=2T,gres/gpu=0",
			expected: TrackableResources{CPUCount: 1, MemoryBytes: 2 * 1024 * 1024 * 1024 * 1024, GPUCount: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			res, err := ParseTrackableResources(tt.input)
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tt.input, err)
				return
			}
			if res.CPUCount != tt.expected.CPUCount {
				t.Errorf("cpu count mismatch: got %d, want %d", res.CPUCount, tt.expected.CPUCount)
			}
			if res.MemoryBytes != tt.expected.MemoryBytes {
				t.Errorf("memory bytes mismatch: got %d, want %d", res.MemoryBytes, tt.expected.MemoryBytes)
			}
			if res.GPUCount != tt.expected.GPUCount {
				t.Errorf("gpu count mismatch: got %d, want %d", res.GPUCount, tt.expected.GPUCount)
			}
		})
	}
}

// TestTresListToString_RoundTrip locks in the contract between tresListToString (used on the accounting path)
// and ParseTrackableResources (used downstream by the exporter).
// Memory must round-trip with its MB unit preserved - without the "M" suffix,
// parseMemoryValue would interpret "8192" as bytes (8 KiB) instead of 8 GiB.
func TestTresListToString_RoundTrip(t *testing.T) {
	list := api.V0041TresList{
		{Type: "cpu", Count: ptr.To(int64(4))},
		{Type: "mem", Count: ptr.To(int64(8192))},
		{Type: "gres", Name: ptr.To("gpu"), Count: ptr.To(int64(2))},
	}

	got := tresListToString(&list)
	assert.Equal(t, "cpu=4,mem=8192M,gres/gpu=2", got)

	parsed, err := ParseTrackableResources(got)
	require.NoError(t, err)
	assert.Equal(t, 4, parsed.CPUCount)
	assert.Equal(t, 8192*1024*1024, parsed.MemoryBytes)
	assert.Equal(t, 2, parsed.GPUCount)
}

func TestTresListToString_NilInput(t *testing.T) {
	assert.Equal(t, "", tresListToString(nil))
}

func TestParseTrackableResources_Failure(t *testing.T) {
	tests := []struct {
		input string
		name  string
	}{
		{input: "cpu=abc,mem=1G", name: "invalid cpu"},
		{input: "cpu=2,mem=xyz", name: "invalid mem"},
		{input: "cpu=2,mem=1X", name: "unknown mem suffix"},
		{input: "cpu=2,mem=,gres/gpu=1", name: "empty mem"},
		{input: "cpu=2,mem=1G,gres/gpu=notanint", name: "invalid gpu"},
		{input: "cpu=,mem=1G", name: "empty cpu"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTrackableResources(tt.input)
			if err == nil {
				t.Errorf("expected error for input %q, got nil", tt.input)
			}
		})
	}
}
