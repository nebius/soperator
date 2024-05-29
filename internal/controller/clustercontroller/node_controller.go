package clustercontroller

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/render/controller"
	"nebius.ai/slurm-operator/internal/values"
)

// DeployControllers creates all resources necessary for deploying Slurm controllers.
func (r SlurmClusterReconciler) DeployControllers(ctx context.Context, clusterValues *values.SlurmCluster, clusterCR *slurmv1.SlurmCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Deploy K8sService
	{
		found := &corev1.Service{}
		dep := controller.RenderService(clusterValues)
		if res, err := r.EnsureDeployed(ctx, &dep, found, clusterCR); err != nil {
			return res, err
		}
	}

	// Deploy StatefulSet
	{
		found := &appsv1.StatefulSet{}
		dep, err := controller.RenderStatefulSet(clusterValues)
		if err != nil {
			logger.Error(err, "Controller StatefulSet deployment failed")
			return ctrl.Result{}, err
		}
		dependencies, err := r.getControllersStatefulSetDependencies(ctx, clusterValues)
		if err != nil {
			logger.Error(err, "Failed at retrieving dependencies for the controller StatefulSet")
			return ctrl.Result{}, err
		}
		if res, err := r.EnsureDeployed(ctx, &dep, found, clusterCR, dependencies...); err != nil {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// UpdateControllers makes sure that Slurm controllers are up-to-date with CRD.
func (r SlurmClusterReconciler) UpdateControllers(ctx context.Context, clusterValues *values.SlurmCluster, clusterCR *slurmv1.SlurmCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	{
		existing := &appsv1.StatefulSet{}
		updated, err := controller.RenderStatefulSet(clusterValues)
		if err != nil {
			logger.Error(err, "Controller StatefulSet update failed")
			return ctrl.Result{}, err
		}
		dependencies, err := r.getControllersStatefulSetDependencies(ctx, clusterValues)
		if err != nil {
			logger.Error(err, "Failed at retrieving dependencies for the controller StatefulSet")
			return ctrl.Result{}, err
		}
		if res, err := r.EnsureUpdated(ctx, &updated, existing, clusterCR, dependencies...); err != nil {
			return res, err
		}
		return ctrl.Result{}, err
	}
}

// ValidateControllers checks that Slurm controllers are reconciled with the desired state correctly
func (r SlurmClusterReconciler) ValidateControllers(ctx context.Context, clusterValues *values.SlurmCluster, clusterCR *slurmv1.SlurmCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	found := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: clusterValues.NodeController.StatefulSet.Name, Namespace: clusterValues.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
	} else if err != nil {
		logger.Error(err, "Failed to get controller StatefulSet")
		return ctrl.Result{}, err
	}

	targetReplicas := clusterValues.NodeController.StatefulSet.Replicas
	if found.Spec.Replicas != nil {
		targetReplicas = *found.Spec.Replicas
	}
	if found.Status.AvailableReplicas != targetReplicas {
		meta.SetStatusCondition(&clusterCR.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterControllersAvailable,
			Status: metav1.ConditionFalse, Reason: "NotAvailable",
			Message: "Slurm controllers are not available yet",
		})
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		meta.SetStatusCondition(&clusterCR.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterControllersAvailable,
			Status: metav1.ConditionTrue, Reason: "Available",
			Message: "Slurm controllers are available",
		})
	}

	return ctrl.Result{}, nil
}

func (r SlurmClusterReconciler) getControllersStatefulSetDependencies(ctx context.Context, clusterValues *values.SlurmCluster) ([]metav1.Object, error) {
	var res []metav1.Object

	slurmConfigsConfigMap := &corev1.ConfigMap{}
	if err := r.GetNamespacedObject(ctx, clusterValues.ConfigMapSlurmConfigs.Name, clusterValues.Namespace, slurmConfigsConfigMap); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, slurmConfigsConfigMap)

	slurmKeySecret := &corev1.Secret{}
	if err := r.GetNamespacedObject(ctx, clusterValues.Secrets.SlurmKey.Name, clusterValues.Namespace, slurmKeySecret); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, slurmKeySecret)

	// SSH public keys secret is a dependency if login nodes are used
	if clusterValues.Secrets.SSHPublicKeys != nil {
		secret := &corev1.Secret{}
		if err := r.GetNamespacedObject(ctx, clusterValues.Secrets.SSHPublicKeys.Name, clusterValues.Namespace, secret); err != nil {
			return []metav1.Object{}, err
		}
		res = append(res, secret)
	}

	return res, nil
}
