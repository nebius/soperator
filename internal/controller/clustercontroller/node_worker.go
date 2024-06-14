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
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/values"
)

// DeployWorkers creates all resources necessary for deploying Slurm workers
func (r SlurmClusterReconciler) DeployWorkers(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	clusterCR *slurmv1.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Deploy NCCL topology ConfigMap
	{
		found := &corev1.ConfigMap{}
		dep, err := worker.RenderConfigMapNCCLTopology(clusterValues)
		if err != nil {
			return ctrl.Result{}, err
		}
		if res, err := r.EnsureDeployed(ctx, &dep, found, clusterCR); err != nil {
			return res, err
		}
	}

	// Deploy Service
	{
		found := &corev1.Service{}
		dep := worker.RenderService(clusterValues.Namespace, clusterValues.Name, &clusterValues.NodeWorker)
		if res, err := r.EnsureDeployed(ctx, &dep, found, clusterCR); err != nil {
			return res, err
		}
	}

	// Deploy StatefulSet
	{
		found := &appsv1.StatefulSet{}
		dep, err := worker.RenderStatefulSet(
			clusterValues.Namespace,
			clusterValues.Name,
			clusterValues.NodeFilters,
			&clusterValues.Secrets,
			clusterValues.VolumeSources,
			&clusterValues.NodeWorker,
		)
		if err != nil {
			logger.Error(err, "Worker StatefulSet deployment failed")
			return ctrl.Result{}, err
		}
		dependencies, err := r.getWorkersStatefulSetDependencies(ctx, clusterValues)
		if err != nil {
			logger.Error(err, "Failed at retrieving dependencies for the worker StatefulSet")
			return ctrl.Result{}, err
		}
		if res, err := r.EnsureDeployed(ctx, &dep, found, clusterCR, dependencies...); err != nil {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// UpdateWorkers makes sure that Slurm workers are up-to-date with CRD
func (r SlurmClusterReconciler) UpdateWorkers(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	clusterCR *slurmv1.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	existing := &appsv1.StatefulSet{}
	updated, err := worker.RenderStatefulSet(
		clusterValues.Namespace,
		clusterValues.Name,
		clusterValues.NodeFilters,
		&clusterValues.Secrets,
		clusterValues.VolumeSources,
		&clusterValues.NodeWorker,
	)
	if err != nil {
		logger.Error(err, "Worker StatefulSet update failed")
		return ctrl.Result{}, err
	}

	dependencies, err := r.getWorkersStatefulSetDependencies(ctx, clusterValues)
	if err != nil {
		logger.Error(err, "Failed at retrieving dependencies for the worker StatefulSet")
		return ctrl.Result{}, err
	}

	if res, err := r.EnsureUpdated(ctx, &updated, existing, clusterCR, dependencies...); err != nil {
		return res, err
	}

	return ctrl.Result{}, nil
}

// ValidateWorkers checks that Slurm workers are reconciled with the desired state correctly
func (r SlurmClusterReconciler) ValidateWorkers(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	clusterCR *slurmv1.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	found := &appsv1.StatefulSet{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Name:      clusterValues.NodeWorker.StatefulSet.Name,
			Namespace: clusterValues.Namespace,
		},
		found,
	)
	if err != nil && apierrors.IsNotFound(err) {
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
	} else if err != nil {
		logger.Error(err, "Failed to get worker StatefulSet")
		return ctrl.Result{}, err
	}

	targetReplicas := clusterValues.NodeWorker.StatefulSet.Replicas
	if found.Spec.Replicas != nil {
		targetReplicas = *found.Spec.Replicas
	}
	if found.Status.AvailableReplicas != targetReplicas {
		meta.SetStatusCondition(&clusterCR.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterWorkersAvailable,
			Status: metav1.ConditionFalse, Reason: "NotAvailable",
			Message: "Slurm workers are not available yet",
		})
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		meta.SetStatusCondition(&clusterCR.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterWorkersAvailable,
			Status: metav1.ConditionTrue, Reason: "Available",
			Message: "Slurm workers are available",
		})
	}

	return ctrl.Result{}, nil
}

func (r SlurmClusterReconciler) getWorkersStatefulSetDependencies(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
) ([]metav1.Object, error) {
	var res []metav1.Object

	slurmConfigsConfigMap := &corev1.ConfigMap{}
	if err := r.GetNamespacedObject(
		ctx,
		clusterValues.Namespace,
		naming.BuildConfigMapSlurmConfigsName(clusterValues.Name),
		slurmConfigsConfigMap,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, slurmConfigsConfigMap)

	mungeKeySecret := &corev1.Secret{}
	if err := r.GetNamespacedObject(
		ctx,
		clusterValues.Namespace,
		clusterValues.Secrets.MungeKey.Name,
		mungeKeySecret,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, mungeKeySecret)

	return res, nil
}
