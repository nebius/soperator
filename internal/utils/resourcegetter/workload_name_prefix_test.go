package resourcegetter

import (
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func TestResolveWorkloadNamePrefix(t *testing.T) {
	tests := []struct {
		name     string
		mode     slurmv1.WorkloadNamePrefixMode
		expected string
	}{
		{
			name:     "enabled",
			mode:     slurmv1.WorkloadNamePrefixEnabled,
			expected: "cluster",
		},
		{
			name:     "disabled",
			mode:     slurmv1.WorkloadNamePrefixDisabled,
			expected: "",
		},
		{
			name:     "empty defaults to enabled",
			expected: "cluster",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, ResolveWorkloadNamePrefix("cluster", test.mode))
		})
	}
}
