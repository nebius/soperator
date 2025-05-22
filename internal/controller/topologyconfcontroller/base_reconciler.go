package topologyconfcontroller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
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

func (r *BaseReconciler) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	return r.Client.Get(ctx, key, obj)
}

func (r *BaseReconciler) Create(ctx context.Context, obj client.Object) error {
	return r.Client.Create(ctx, obj)
}

func (r *BaseReconciler) Update(ctx context.Context, obj client.Object) error {
	return r.Client.Update(ctx, obj)
}

func (r *BaseReconciler) Patch(ctx context.Context, existing, desired client.Object) error {
	patchImpl := func(dst, src *appsv1.DaemonSet) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec.Template.Spec = src.Spec.Template.Spec
		return res
	}
	patch := patchImpl(existing.(*appsv1.DaemonSet), desired.(*appsv1.DaemonSet))
	return r.Client.Patch(ctx, existing, patch)
}

// GetOrCreateConfigMap gets an existing ConfigMap or creates a new one if it doesn't exist.
func (r *BaseReconciler) GetOrCreateConfigMap(
	ctx context.Context,
	configMap *corev1.ConfigMap,
	owner client.Object,
) error {
	logger := log.FromContext(ctx).WithValues(
		"configmap", fmt.Sprintf("%s/%s", configMap.Namespace, configMap.Name),
	)
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
	if client.IgnoreNotFound(err) != nil {
		logger.Error(err, "Failed to get ConfigMap")
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
			logger.Error(err, "Failed to set controller reference for ConfigMap")
			return fmt.Errorf("failed to set controller reference: %w", err)
		}
	}

	if err := r.Client.Create(ctx, configMap); err != nil {
		logger.Error(err, "Failed to create ConfigMap")
		return fmt.Errorf("failed to create ConfigMap %s/%s: %w", configMap.Namespace, configMap.Name, err)
	}

	logger.Info("Successfully created ConfigMap")
	return nil
}
