package pattern

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		name     string
		merged   string
		expected []string
	}{
		{name: "empty", merged: "", expected: nil},
		{name: "blank", merged: "   ", expected: nil},
		{name: "single entity", merged: "worker-0", expected: []string{"worker-0"}},
		{name: "range", merged: "worker-[0-2]", expected: []string{"worker-0", "worker-1", "worker-2"}},
		{
			name:     "range with gaps",
			merged:   "worker-[0-1,5]",
			expected: []string{"worker-0", "worker-1", "worker-5"},
		},
		{
			name:     "several prefixes",
			merged:   "cpu-[0-1],gpu-0",
			expected: []string{"cpu-0", "cpu-1", "gpu-0"},
		},
		{
			name:     "padded range keeps its width",
			merged:   "worker-[08-10]",
			expected: []string{"worker-08", "worker-09", "worker-10"},
		},
		{
			name:     "unparsable range is kept whole",
			merged:   "worker-[a-b]",
			expected: []string{"worker-[a-b]"},
		},
		{
			name:     "entity without a numeric suffix",
			merged:   "login",
			expected: []string{"login"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, Expand(tt.merged))
		})
	}
}

// Expand has to read back everything Merge writes, since the two are used on the two sides of the
// same comparison.
func TestExpandRoundTripsMerge(t *testing.T) {
	tests := [][]string{
		{"worker-0"},
		{"worker-0", "worker-1", "worker-2"},
		{"worker-0", "worker-1", "worker-5", "worker-6"},
		{"cpu-0", "cpu-1", "gpu-0", "gpu-3"},
		{"worker-08", "worker-09", "worker-10"},
	}

	for _, entities := range tests {
		merged := Merge(entities)
		assert.Equal(t, entities, Expand(merged), "merged as %q", merged)
	}
}
