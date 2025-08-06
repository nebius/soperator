package topologyconfcontroller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type BaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// GetOrCreateConfigMap gets an existing ConfigMap or creates a new one if it doesn't exist.
func (r *BaseReconciler) GetOrCreateConfigMap(
	ctx context.Context,
	configMap *corev1.ConfigMap,
	owner client.Object,
) error {
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
	if client.IgnoreNotFound(err) != nil {
		return err
	}

	// ConfigMap already exists
	if err == nil {
		return nil
	}

	return r.CreateConfigMap(ctx, configMap, owner)
}

// CreateConfigMap creates a new ConfigMap, optionally setting the owner reference.
func (r *BaseReconciler) CreateConfigMap(
	ctx context.Context,
	configMap *corev1.ConfigMap,
	owner client.Object,
) error {
	logger := log.FromContext(ctx)
	logger.Info("Creating ConfigMap", "name", configMap.Name, "namespace", configMap.Namespace)

	if owner != nil {
		if err := ctrl.SetControllerReference(owner, configMap, r.Scheme); err != nil {
			return fmt.Errorf("failed to set controller reference: %w", err)
		}
	}

	if err := r.Client.Create(ctx, configMap, client.FieldOwner(WorkerTopologyReconcilerName)); err != nil {
		return fmt.Errorf("failed to create ConfigMap %s/%s: %w", configMap.Namespace, configMap.Name, err)
	}

	logger.Info("Successfully created ConfigMap")
	return nil
}
