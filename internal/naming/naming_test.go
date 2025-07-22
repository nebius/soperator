package naming

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/consts"
)

func TestBuildConfigMapSecurityLimitsName(t *testing.T) {
	tests := []struct {
		name          string
		componentType consts.ComponentType
		clusterName   string
		expected      string
	}{
		{
			name:          "Login component",
			componentType: consts.ComponentTypeLogin,
			clusterName:   "test-cluster",
			expected:      "test-cluster-login-security-limits",
		},
		{
			name:          "Worker component",
			componentType: consts.ComponentTypeWorker,
			clusterName:   "test-cluster",
			expected:      "test-cluster-worker-security-limits",
		},
		{
			name:          "Controller component",
			componentType: consts.ComponentTypeController,
			clusterName:   "test-cluster",
			expected:      "test-cluster-controller-security-limits",
		},
		{
			name:          "Exporter component",
			componentType: consts.ComponentTypeExporter,
			clusterName:   "test-cluster",
			expected:      "test-cluster-exporter-security-limits",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildConfigMapSecurityLimitsName(tt.componentType, tt.clusterName)
			assert.Equal(t, tt.expected, result)
		})
	}
}
