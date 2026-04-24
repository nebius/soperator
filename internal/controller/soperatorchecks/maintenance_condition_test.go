package soperatorchecks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

func TestMaintenanceConditionTypeConfiguration(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)
	slurmAPIClients := slurmapi.NewClientSet()

	tests := []struct {
		name                  string
		inputConditionType    string
		expectedConditionType string
		description           string
	}{
		{
			name:                  "default value when empty string provided",
			inputConditionType:    "",
			expectedConditionType: string(consts.DefaultMaintenanceConditionType),
			description:           "Should use default when empty string is provided",
		},
		{
			name:                  "custom value is preserved",
			inputConditionType:    "CustomMaintenanceScheduled",
			expectedConditionType: "CustomMaintenanceScheduled",
			description:           "Should preserve custom maintenance condition type",
		},
		{
			name:                  "another custom value",
			inputConditionType:    "MyCustomCondition",
			expectedConditionType: "MyCustomCondition",
			description:           "Should work with any custom condition type",
		},
		{
			name:                  "default value constant matches expected",
			inputConditionType:    string(consts.DefaultMaintenanceConditionType),
			expectedConditionType: "NebiusMaintenanceScheduled",
			description:           "Default constant should match expected default value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sController := NewK8SNodesController(
				client,
				scheme,
				recorder,
				15*time.Minute,
				true,
				corev1.NodeConditionType(tt.inputConditionType),
				"",
			)

			assert.Equal(t, tt.expectedConditionType, string(k8sController.MaintenanceConditionType),
				"K8SNodesController: %s", tt.description)

			slurmController := NewSlurmNodesController(
				client,
				scheme,
				recorder,
				slurmAPIClients,
				30*time.Second,
				true,
				true,
				client,
				corev1.NodeConditionType(tt.inputConditionType),
			)

			assert.Equal(t, tt.expectedConditionType, string(slurmController.MaintenanceConditionType),
				"SlurmNodesController: %s", tt.description)
		})
	}
}

func TestDefaultMaintenanceConditionTypeConstant(t *testing.T) {

	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)
	slurmAPIClients := slurmapi.NewClientSet()

	k8sController := NewK8SNodesController(client, scheme, recorder, 15*time.Minute, true, "", "")
	slurmController := NewSlurmNodesController(client, scheme, recorder, slurmAPIClients, 30*time.Second, true, true, client, "")

	expectedDefault := string(consts.DefaultMaintenanceConditionType)

	assert.Equal(t, expectedDefault, string(k8sController.MaintenanceConditionType),
		"K8SNodesController should use default maintenance condition type")
	assert.Equal(t, expectedDefault, string(slurmController.MaintenanceConditionType),
		"SlurmNodesController should use default maintenance condition type")
}
