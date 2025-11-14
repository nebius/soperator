package naming_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
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
			result := naming.BuildConfigMapSecurityLimitsName(tt.componentType, tt.clusterName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildConfigMapSecurityLimitsForNodeSetName(t *testing.T) {
	clusterName := "test-cluster"
	template := "%s-nodeset-%s-security-limits"

	tests := []struct {
		nodeSetName string
	}{
		{
			nodeSetName: "worker",
		},
		{
			nodeSetName: "worker-cpu",
		},
		{
			nodeSetName: "gpu",
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("Nodeset %s", tt.nodeSetName), func(t *testing.T) {
			result := naming.BuildConfigMapSecurityLimitsForNodeSetName(clusterName, tt.nodeSetName)
			assert.Equal(t, fmt.Sprintf(template, clusterName, tt.nodeSetName), result)
		})
	}
}
