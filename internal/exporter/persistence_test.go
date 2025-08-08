package exporter

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

func TestLoadNotUsableTimestamps_NoConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	configMapName := types.NamespacedName{
		Name:      "test-exporter-state",
		Namespace: "test-namespace",
	}

	timestamps, err := LoadNotUsableTimestamps(context.Background(), fakeClient, configMapName)

	require.NoError(t, err)
	assert.Empty(t, timestamps)
}

func TestLoadNotUsableTimestamps_EmptyConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-exporter-state",
			Namespace: "test-namespace",
		},
		Data: map[string]string{},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()
	configMapName := types.NamespacedName{
		Name:      "test-exporter-state",
		Namespace: "test-namespace",
	}

	timestamps, err := LoadNotUsableTimestamps(context.Background(), fakeClient, configMapName)

	require.NoError(t, err)
	assert.Empty(t, timestamps)
}

func TestLoadNotUsableTimestamps_WithData(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	testTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	timestampData := map[string]string{
		"worker-0": testTime.Format(time.RFC3339),
		"worker-1": testTime.Add(time.Hour).Format(time.RFC3339),
	}
	jsonData, err := json.Marshal(timestampData)
	require.NoError(t, err)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-exporter-state",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			ConfigMapKey: string(jsonData),
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()
	configMapName := types.NamespacedName{
		Name:      "test-exporter-state",
		Namespace: "test-namespace",
	}

	timestamps, err := LoadNotUsableTimestamps(context.Background(), fakeClient, configMapName)

	require.NoError(t, err)
	assert.Len(t, timestamps, 2)
	assert.Equal(t, testTime, timestamps["worker-0"])
	assert.Equal(t, testTime.Add(time.Hour), timestamps["worker-1"])
}

func TestLoadNotUsableTimestamps_InvalidJSON(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-exporter-state",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			ConfigMapKey: "invalid json",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()
	configMapName := types.NamespacedName{
		Name:      "test-exporter-state",
		Namespace: "test-namespace",
	}

	timestamps, err := LoadNotUsableTimestamps(context.Background(), fakeClient, configMapName)

	require.NoError(t, err)
	assert.Empty(t, timestamps) // Should return empty map on parse error
}

func TestSaveNotUsableTimestamps_CreateNew(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	configMapName := types.NamespacedName{
		Name:      "test-exporter-state",
		Namespace: "test-namespace",
	}

	testTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	timestamps := map[string]time.Time{
		"worker-0": testTime,
		"worker-1": testTime.Add(time.Hour),
	}

	// Create some mock nodes for cleanup
	nodes := []slurmapi.Node{
		{Name: "worker-0"},
		{Name: "worker-1"},
	}

	err := SaveNotUsableTimestamps(context.Background(), fakeClient, configMapName, timestamps, nodes)
	require.NoError(t, err)

	// Verify ConfigMap was created
	var configMap corev1.ConfigMap
	err = fakeClient.Get(context.Background(), configMapName, &configMap)
	require.NoError(t, err)

	assert.Contains(t, configMap.Data, ConfigMapKey)

	// Verify the data is correct
	var storedTimestamps map[string]string
	err = json.Unmarshal([]byte(configMap.Data[ConfigMapKey]), &storedTimestamps)
	require.NoError(t, err)

	assert.Equal(t, testTime.Format(time.RFC3339), storedTimestamps["worker-0"])
	assert.Equal(t, testTime.Add(time.Hour).Format(time.RFC3339), storedTimestamps["worker-1"])
}

func TestSaveNotUsableTimestamps_Update(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	// Create existing ConfigMap with old data
	oldTime := time.Date(2025, 1, 14, 10, 0, 0, 0, time.UTC)
	oldTimestampData := map[string]string{
		"worker-0": oldTime.Format(time.RFC3339),
	}
	oldJsonData, err := json.Marshal(oldTimestampData)
	require.NoError(t, err)

	existingConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-exporter-state",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			ConfigMapKey: string(oldJsonData),
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingConfigMap).Build()
	configMapName := types.NamespacedName{
		Name:      "test-exporter-state",
		Namespace: "test-namespace",
	}

	// Update with new data
	newTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	timestamps := map[string]time.Time{
		"worker-0": oldTime, // Keep old timestamp
		"worker-1": newTime, // Add new timestamp
	}

	// Create nodes for cleanup
	nodes := []slurmapi.Node{
		{Name: "worker-0"},
		{Name: "worker-1"},
	}

	err = SaveNotUsableTimestamps(context.Background(), fakeClient, configMapName, timestamps, nodes)
	require.NoError(t, err)

	// Verify ConfigMap was updated
	var configMap corev1.ConfigMap
	err = fakeClient.Get(context.Background(), configMapName, &configMap)
	require.NoError(t, err)

	var storedTimestamps map[string]string
	err = json.Unmarshal([]byte(configMap.Data[ConfigMapKey]), &storedTimestamps)
	require.NoError(t, err)

	assert.Len(t, storedTimestamps, 2)
	assert.Equal(t, oldTime.Format(time.RFC3339), storedTimestamps["worker-0"])
	assert.Equal(t, newTime.Format(time.RFC3339), storedTimestamps["worker-1"])
}

func TestSaveNotUsableTimestamps_CleanupNonExistentNodes(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	configMapName := types.NamespacedName{
		Name:      "test-exporter-state",
		Namespace: "test-namespace",
	}

	testTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	// Timestamps include a node that doesn't exist anymore
	timestamps := map[string]time.Time{
		"worker-0":     testTime,
		"worker-1":     testTime.Add(time.Hour),
		"old-worker-2": testTime.Add(2 * time.Hour), // This node no longer exists
	}

	// Current nodes only include worker-0 and worker-1
	nodes := []slurmapi.Node{
		{Name: "worker-0"},
		{Name: "worker-1"},
	}

	err := SaveNotUsableTimestamps(context.Background(), fakeClient, configMapName, timestamps, nodes)
	require.NoError(t, err)

	// Verify ConfigMap was created and cleaned up
	var configMap corev1.ConfigMap
	err = fakeClient.Get(context.Background(), configMapName, &configMap)
	require.NoError(t, err)

	var storedTimestamps map[string]string
	err = json.Unmarshal([]byte(configMap.Data[ConfigMapKey]), &storedTimestamps)
	require.NoError(t, err)

	// Should only contain worker-0 and worker-1, old-worker-2 should be cleaned up
	assert.Len(t, storedTimestamps, 2)
	assert.Equal(t, testTime.Format(time.RFC3339), storedTimestamps["worker-0"])
	assert.Equal(t, testTime.Add(time.Hour).Format(time.RFC3339), storedTimestamps["worker-1"])
	assert.NotContains(t, storedTimestamps, "old-worker-2")
}

func TestSaveNotUsableTimestamps_NoChangeSkipsUpdate(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	testTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	timestampData := map[string]string{
		"worker-0": testTime.Format(time.RFC3339),
		"worker-1": testTime.Add(time.Hour).Format(time.RFC3339),
	}
	jsonData, err := json.Marshal(timestampData)
	require.NoError(t, err)

	existingConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-exporter-state",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			ConfigMapKey: string(jsonData),
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingConfigMap).Build()
	configMapName := types.NamespacedName{
		Name:      "test-exporter-state",
		Namespace: "test-namespace",
	}

	// Save identical timestamps
	timestamps := map[string]time.Time{
		"worker-0": testTime,
		"worker-1": testTime.Add(time.Hour),
	}

	nodes := []slurmapi.Node{
		{Name: "worker-0"},
		{Name: "worker-1"},
	}

	err = SaveNotUsableTimestamps(context.Background(), fakeClient, configMapName, timestamps, nodes)
	require.NoError(t, err)

	// Verify ConfigMap still exists with same data - function should have exited early
	var configMap corev1.ConfigMap
	err = fakeClient.Get(context.Background(), configMapName, &configMap)
	require.NoError(t, err)

	assert.Equal(t, string(jsonData), configMap.Data[ConfigMapKey])
}
