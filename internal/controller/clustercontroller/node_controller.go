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
	"nebius.ai/slurm-operator/internal/models/slurm"
	"nebius.ai/slurm-operator/internal/node/controller"
)

// DeployControllers creates all resources necessary for deploying Slurm controllers.
func (r SlurmClusterReconciler) DeployControllers(ctx context.Context, cv *smodels.ClusterValues, cluster *slurmv1.SlurmCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Deploy Service
	{
		found := &corev1.Service{}
		dep := controller.RenderService(*cv)
		if res, err := r.EnsureDeployed(ctx, &dep, found, cluster); err != nil {
			return res, err
		}
	}

	// Deploy ConfigMap
	{
		found := &corev1.ConfigMap{}
		dep := controller.RenderConfigMap(*cv)
		if res, err := r.EnsureDeployed(ctx, &dep, found, cluster); err != nil {
			return res, err
		}
	}

	// Deploy StatefulSet
	{
		found := &appsv1.StatefulSet{}
		dep, err := controller.RenderStatefulSet(*cv)
		if err != nil {
			logger.Error(err, "Controller StatefulSet creation failed")
			return ctrl.Result{}, err
		}
		// TODO add dependencies
		if res, err := r.EnsureDeployed(ctx, &dep, found, cluster); err != nil {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// UpdateControllers makes sure that Slurm controllers are up-to-date with CRD.
func (r SlurmClusterReconciler) UpdateControllers(ctx context.Context, cv *smodels.ClusterValues, cluster *slurmv1.SlurmCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	{
		existing := &appsv1.StatefulSet{}
		updated, err := controller.RenderStatefulSet(*cv)
		if err != nil {
			logger.Error(err, "Controller StatefulSet creation failed")
			return ctrl.Result{}, err
		}
		// TODO add dependencies
		if res, err := r.EnsureUpdated(ctx, &updated, existing, cluster); err != nil {
			return res, err
		}
		return ctrl.Result{}, err
	}
}

// ValidateControllers checks that Slurm controllers are reconciled with the desired state correctly
func (r SlurmClusterReconciler) ValidateControllers(ctx context.Context, cv *smodels.ClusterValues, cluster *slurmv1.SlurmCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	found := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: cv.Controller.StatefulSet.Name, Namespace: cv.Controller.StatefulSet.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
	} else if err != nil {
		logger.Error(err, "Failed to get Slurm controller StatefulSet")
		return ctrl.Result{}, err
	}

	targetReplicas := cv.Controller.StatefulSet.Replicas
	if found.Spec.Replicas != nil {
		targetReplicas = *found.Spec.Replicas
	}
	if found.Status.AvailableReplicas != targetReplicas {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterControllersAvailable,
			Status: metav1.ConditionFalse, Reason: "NotAvailable",
			Message: "Slurm controllers are not available yet",
		})
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterControllersAvailable,
			Status: metav1.ConditionTrue, Reason: "Available",
			Message: "Slurm controllers are available",
		})
	}

	return ctrl.Result{}, nil
}
