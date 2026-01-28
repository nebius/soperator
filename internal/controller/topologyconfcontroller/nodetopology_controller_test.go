package topologyconfcontroller_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	kruisev1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	tc "nebius.ai/slurm-operator/internal/controller/topologyconfcontroller"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestExtractTierLabels(t *testing.T) {
	// Test data
	k8sNodeLabels := map[string]string{
		consts.DefaultTopologyLabelPrefix + "/tier-0": "nvl0",
		consts.DefaultTopologyLabelPrefix + "/tier-1": "leaf00",
		consts.DefaultTopologyLabelPrefix + "/other":  "value",
		consts.DefaultTopologyLabelPrefix + "/tier-2": "spine00",
		"unrelated.label": "unrelatedValue",
	}

	// Expected result
	expected := map[string]string{
		"tier-0": "nvl0",
		"tier-1": "leaf00",
		"tier-2": "spine00",
	}

	// Call the function
	result := tc.ExtractTierLabels(k8sNodeLabels, consts.DefaultTopologyLabelPrefix)

	// Validate the result
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("ExtractTierLabels() = %v, want %v", result, expected)
	}
}

func TestUpdateTopologyConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = kruisev1alpha1.AddToScheme(scheme)
	_ = slurmv1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	reconciler := &tc.NodeTopologyReconciler{
		BaseReconciler: tc.BaseReconciler{
			Client: fakeClient,
			Scheme: scheme,
		},
		Namespace: "default",
		// Use the same fakeClient as APIReader for tests
		APIReader: fakeClient,
	}

	ctx := context.TODO()

	tests := []struct {
		name     string
		nodeName string
		tierData map[string]string
		expected map[string]string
	}{
		{
			name:     "Add node data",
			nodeName: "node-1",
			tierData: map[string]string{"tier-1": "foo", "tier-2": "bar"},
			expected: map[string]string{"node-1": `{"tier-1":"foo","tier-2":"bar"}`},
		},
		{
			name:     "Update node data",
			nodeName: "node-1",
			tierData: map[string]string{"tier-1": "foo", "tier-2": "baz"},
			expected: map[string]string{"node-1": `{"tier-1":"foo","tier-2":"baz"}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get or create the ResourceDistribution first
			rd, err := reconciler.GetOrCreateTopologyResourceDistribution(ctx)
			assert.NoError(t, err)

			err = reconciler.UpdateResourceDistribution(ctx, tt.nodeName, tt.tierData, rd)
			assert.NoError(t, err)

			// Get updated ResourceDistribution
			updatedRd := &kruisev1alpha1.ResourceDistribution{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name: consts.ResourceDistributionNameTopology}, updatedRd)
			assert.NoError(t, err)

			// Extract ConfigMap data from ResourceDistribution
			var configMap corev1.ConfigMap
			err = json.Unmarshal(updatedRd.Spec.Resource.Raw, &configMap)
			assert.NoError(t, err)

			assert.Equal(t, tt.expected, configMap.Data)
		})
	}
}

func TestRemoveTopologyConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = kruisev1alpha1.AddToScheme(scheme)
	_ = slurmv1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	reconciler := &tc.NodeTopologyReconciler{
		BaseReconciler: tc.BaseReconciler{
			Client: fakeClient,
			Scheme: scheme,
		},
		Namespace: "default",
		// Use the same fakeClient as APIReader for tests
		APIReader: fakeClient,
	}

	ctx := context.TODO()

	tests := []struct {
		name     string
		nodeName string
		expected map[string]string
	}{
		{
			name:     "Delete node data",
			nodeName: "node-1",
			expected: nil, // After JSON marshal/unmarshal, empty map becomes nil
		},
	}

	for _, tt := range tests {

		tierData := map[string]string{"tier-1": "foo", "tier-2": "bar"}
		// Get or create the ResourceDistribution first
		rd, err := reconciler.GetOrCreateTopologyResourceDistribution(ctx)
		assert.NoError(t, err)

		err = reconciler.UpdateResourceDistribution(ctx, tt.nodeName, tierData, rd)
		logger := log.FromContext(ctx).WithName(tc.NodeTopologyReconcilerName)
		assert.NoError(t, err)
		t.Run(tt.name, func(t *testing.T) {
			// Get the ResourceDistribution again for removal
			rd, err := reconciler.GetOrCreateTopologyResourceDistribution(ctx)
			assert.NoError(t, err)

			err = reconciler.RemoveNodeFromResourceDistribution(ctx, tt.nodeName, rd, logger)
			assert.NoError(t, err)

			// Get updated ResourceDistribution
			updatedRd := &kruisev1alpha1.ResourceDistribution{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name: consts.ResourceDistributionNameTopology}, updatedRd)
			assert.NoError(t, err)

			// Extract ConfigMap data from ResourceDistribution
			var configMap corev1.ConfigMap
			err = json.Unmarshal(updatedRd.Spec.Resource.Raw, &configMap)
			assert.NoError(t, err)

			assert.Equal(t, tt.expected, configMap.Data)
		})
	}
}
