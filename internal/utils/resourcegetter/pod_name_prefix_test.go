package resourcegetter

import (
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func TestResolvePodNamePrefix(t *testing.T) {
	tests := []struct {
		name     string
		mode     slurmv1.PodNamePrefixMode
		expected string
	}{
		{
			name:     "enabled",
			mode:     slurmv1.PodNamePrefixEnabled,
			expected: "cluster",
		},
		{
			name:     "disabled",
			mode:     slurmv1.PodNamePrefixDisabled,
			expected: "",
		},
		{
			name:     "empty defaults to enabled",
			expected: "cluster",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, ResolvePodNamePrefix("cluster", test.mode))
		})
	}
}
