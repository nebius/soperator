package clustercontroller

import (
	"context"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/values"
)

// ReconcileWorkers reconciles all resources necessary for deploying Slurm workers
func (r SlurmClusterReconciler) ReconcileWorkers(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) error {
	logger := log.FromContext(ctx)

	reconcileWorkersImpl := func() error {
		// NCCL topology ConfigMap
		{
			desired, err := worker.RenderConfigMapNCCLTopology(clusterValues)
			if err != nil {
				logger.Error(err, "Failed to render worker NCCL topology ConfigMap")
				return errors.Wrap(err, "rendering worker NCCL topology ConfigMap")
			}
			logger = logger.WithValues(logfield.ResourceKV(&desired)...)
			err = r.ConfigMap.Reconcile(ctx, cluster, &desired)
			if err != nil {
				logger.Error(err, "Failed to reconcile worker NCCL topology ConfigMap")
				return errors.Wrap(err, "reconciling worker NCCL topology ConfigMap")
			}
		}

		// Service
		{
			desired := worker.RenderService(clusterValues.Namespace, clusterValues.Name, &clusterValues.NodeWorker)
			logger = logger.WithValues(logfield.ResourceKV(&desired)...)
			err := r.Service.Reconcile(ctx, cluster, &desired)
			if err != nil {
				logger.Error(err, "Failed to reconcile worker Service")
				return errors.Wrap(err, "reconciling worker Service")
			}
		}

		// StatefulSet
		{
			desired, err := worker.RenderStatefulSet(
				clusterValues.Namespace,
				clusterValues.Name,
				clusterValues.NodeFilters,
				&clusterValues.Secrets,
				clusterValues.VolumeSources,
				&clusterValues.NodeWorker,
			)
			if err != nil {
				logger.Error(err, "Failed to render worker StatefulSet")
				return errors.Wrap(err, "rendering worker StatefulSet")
			}
			logger = logger.WithValues(logfield.ResourceKV(&desired)...)

			deps, err := r.getWorkersStatefulSetDependencies(ctx, clusterValues)
			if err != nil {
				logger.Error(err, "Failed to retrieve dependencies for worker StatefulSet")
				return errors.Wrap(err, "retrieving dependencies for worker StatefulSet")
			}

			err = r.StatefulSet.Reconcile(ctx, cluster, &desired, deps...)
			if err != nil {
				logger.Error(err, "Failed to reconcile worker StatefulSet")
				return errors.Wrap(err, "reconciling worker StatefulSet")
			}
		}

		return nil
	}

	if err := reconcileWorkersImpl(); err != nil {
		logger.Error(err, "Failed to reconcile Slurm workers")
		return errors.Wrap(err, "reconciling Slurm workers")
	}
	return nil
}

// ValidateWorkers checks that Slurm workers are reconciled with the desired state correctly
func (r SlurmClusterReconciler) ValidateWorkers(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	existing := &appsv1.StatefulSet{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      clusterValues.NodeWorker.StatefulSet.Name,
		},
		existing,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
		}
		logger.Error(err, "Failed to get worker StatefulSet")
		return ctrl.Result{}, errors.Wrap(err, "getting worker StatefulSet")
	}

	targetReplicas := clusterValues.NodeWorker.StatefulSet.Replicas
	if existing.Spec.Replicas != nil {
		targetReplicas = *existing.Spec.Replicas
	}
	if existing.Status.AvailableReplicas != targetReplicas {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterWorkersAvailable,
			Status: metav1.ConditionFalse, Reason: "NotAvailable",
			Message: "Slurm workers are not available yet",
		})
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
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
	if err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      naming.BuildConfigMapSlurmConfigsName(clusterValues.Name),
		},
		slurmConfigsConfigMap,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, slurmConfigsConfigMap)

	mungeKeySecret := &corev1.Secret{}
	if err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      clusterValues.Secrets.MungeKey.Name,
		},
		mungeKeySecret,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, mungeKeySecret)

	return res, nil
}
