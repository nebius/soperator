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
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/render/controller"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// ReconcileControllers reconciles all resources necessary for deploying Slurm controllers
func (r SlurmClusterReconciler) ReconcileControllers(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) error {
	logger := log.FromContext(ctx)

	reconcileControllersImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of Slurm Controllers",
			utils.MultiStepExecutionStrategyCollectErrors,

			utils.MultiStepExecutionStep{
				Name: "Slurm Controller Security limits ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					if clusterValues.NodeController.ContainerSlurmctld.NodeContainer.SecurityLimitsConfig == "" {
						return nil
					}

					desired := common.RenderConfigMapSecurityLimits(consts.ComponentTypeController, clusterValues)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err := r.ConfigMap.Reconcile(ctx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling controller security limits configmap")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Controller Service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired := controller.RenderService(clusterValues.Namespace, clusterValues.Name, &clusterValues.NodeController)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err := r.Service.Reconcile(ctx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling controller Service")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Controller StatefulSet",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired, err := controller.RenderStatefulSet(
						clusterValues.Namespace,
						clusterValues.Name,
						clusterValues.NodeFilters,
						&clusterValues.Secrets,
						clusterValues.VolumeSources,
						&clusterValues.NodeController,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering controller StatefulSet")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					deps, err := r.getControllersStatefulSetDependencies(ctx, clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to retrieve dependencies")
						return errors.Wrap(err, "retrieving dependencies for controller StatefulSet")
					}
					stepLogger.Info("Retrieved dependencies")

					if err = r.StatefulSet.Reconcile(ctx, cluster, &desired, deps...); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling controller StatefulSet")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
		)
	}

	if err := reconcileControllersImpl(); err != nil {
		logger.Error(err, "Failed to reconcile Slurm Controllers")
		return errors.Wrap(err, "reconciling Slurm Controllers")
	}
	logger.Info("Reconciled Slurm Controllers")
	return nil
}

// ValidateControllers checks that Slurm controllers are reconciled with the desired state correctly
func (r SlurmClusterReconciler) ValidateControllers(
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
			Name:      clusterValues.NodeController.StatefulSet.Name,
		},
		existing,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
		}
		logger.Error(err, "Failed to get controller StatefulSet")
		return ctrl.Result{}, errors.Wrap(err, "getting controller StatefulSet")
	}

	targetReplicas := clusterValues.NodeController.StatefulSet.Replicas
	if existing.Spec.Replicas != nil {
		targetReplicas = *existing.Spec.Replicas
	}
	if existing.Status.AvailableReplicas != targetReplicas {
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

func (r SlurmClusterReconciler) getControllersStatefulSetDependencies(
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
			Name:      naming.BuildSecretMungeKeyName(clusterValues.Name),
		},
		mungeKeySecret,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, mungeKeySecret)

	return res, nil
}
