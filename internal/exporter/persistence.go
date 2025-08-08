package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

const (
	// ConfigMapKey is the key used to store node not-usable timestamps in the ConfigMap
	ConfigMapKey = "node-not-usable"
)

// LoadNotUsableTimestamps loads node not-usable timestamps from the ConfigMap.
// This should be called only at exporter startup.
func LoadNotUsableTimestamps(ctx context.Context, k8sClient client.Client, configMapName types.NamespacedName) (map[string]time.Time, error) {
	logger := log.FromContext(ctx).WithName(ControllerName).WithValues("configmap", configMapName)

	var configMap corev1.ConfigMap
	err := k8sClient.Get(ctx, configMapName, &configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ConfigMap not found, starting with empty state")
			return make(map[string]time.Time), nil
		}
		return nil, fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	data, exists := configMap.Data[ConfigMapKey]
	if !exists || data == "" {
		logger.Info("ConfigMap exists but has no data, starting with empty state")
		return make(map[string]time.Time), nil
	}

	var timestamps map[string]string
	if err := json.Unmarshal([]byte(data), &timestamps); err != nil {
		logger.Error(err, "Failed to unmarshal ConfigMap data, starting with empty state")
		return make(map[string]time.Time), nil
	}

	result := make(map[string]time.Time, len(timestamps))
	for nodeName, timestampStr := range timestamps {
		timestamp, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			logger.Error(err, "Failed to parse timestamp for node, skipping", "node", nodeName, "timestamp", timestampStr)
			continue
		}
		result[nodeName] = timestamp
	}

	logger.Info("Loaded node not-usable timestamps", "count", len(result))
	return result, nil
}

// SaveNotUsableTimestamps saves node not-usable timestamps to the ConfigMap.
// This should be called on each collection if timestamps were changed.
// It includes cleanup logic to remove nodes that no longer exist in the cluster.
func SaveNotUsableTimestamps(ctx context.Context, k8sClient client.Client, configMapName types.NamespacedName, timestamps map[string]time.Time, currentNodes []slurmapi.Node) error {
	logger := log.FromContext(ctx).WithName(ControllerName).WithValues("configmap", configMapName)

	// Clean up timestamps for nodes that no longer exist in the cluster
	cleanedTimestamps := make(map[string]time.Time, len(timestamps))
	currentNodesMap := make(map[string]struct{}, len(currentNodes))
	for _, node := range currentNodes {
		currentNodesMap[node.Name] = struct{}{}
	}

	cleanedCount := 0
	for nodeName, timestamp := range timestamps {
		if _, exists := currentNodesMap[nodeName]; exists {
			cleanedTimestamps[nodeName] = timestamp
		} else {
			logger.Info("Removing timestamp for node that no longer exists", "node", nodeName)
			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		logger.Info("Cleaned up timestamps for non-existent nodes", "cleaned", cleanedCount)
	}

	// Convert timestamps to string format for JSON serialization
	timestampStrs := make(map[string]string, len(cleanedTimestamps))
	for nodeName, timestamp := range cleanedTimestamps {
		timestampStrs[nodeName] = timestamp.Format(time.RFC3339)
	}

	data, err := json.Marshal(timestampStrs)
	if err != nil {
		return fmt.Errorf("failed to marshal timestamps: %w", err)
	}

	// Try to get existing ConfigMap first
	var existingConfigMap corev1.ConfigMap
	err = k8sClient.Get(ctx, configMapName, &existingConfigMap)

	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get existing ConfigMap: %w", err)
	}

	// Check if the data has changed to avoid unnecessary updates
	if err == nil {
		existingData, exists := existingConfigMap.Data[ConfigMapKey]
		if exists && existingData == string(data) {
			// Data hasn't changed, no need to update
			return nil
		}
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName.Name,
			Namespace: configMapName.Namespace,
		},
		Data: map[string]string{
			ConfigMapKey: string(data),
		},
	}

	if errors.IsNotFound(err) {
		// ConfigMap doesn't exist, create it
		if err := k8sClient.Create(ctx, configMap); err != nil {
			return fmt.Errorf("failed to create ConfigMap: %w", err)
		}
		logger.Info("Created ConfigMap with node not-usable timestamps", "count", len(cleanedTimestamps))
	} else {
		// ConfigMap exists, update it
		if err := k8sClient.Update(ctx, configMap); err != nil {
			return fmt.Errorf("failed to update ConfigMap: %w", err)
		}
		logger.Info("Updated ConfigMap with node not-usable timestamps", "count", len(cleanedTimestamps))
	}

	return nil
}
