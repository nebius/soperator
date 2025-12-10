package clustercontroller

import (
	"context"
	"fmt"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
					stepLogger.V(1).Info("Reconciling")

					desired := common.RenderConfigMapSecurityLimits(consts.ComponentTypeController, clusterValues)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						return fmt.Errorf("reconciling controller security limits configmap: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Controller Service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := controller.RenderService(clusterValues.Namespace, clusterValues.Name, clusterValues.NodeController.Service.Name, &clusterValues.NodeController, nil)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered controller service")

					var controllerNamePtr *string = nil
					if err := r.Service.Reconcile(stepCtx, cluster, &desired, controllerNamePtr); err != nil {
						return fmt.Errorf("reconciling controller Service: %w", err)
					}

					stepLogger.V(1).Info("Reconciled controller service")
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Controller StatefulSet",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired, err := controller.RenderStatefulSet(
						clusterValues.Namespace,
						clusterValues.Name,
						clusterValues.NodeFilters,
						clusterValues.VolumeSources,
						&clusterValues.NodeController,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return fmt.Errorf("rendering controller StatefulSet: %w", err)
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					deps, err := r.getControllersStatefulSetDependencies(stepCtx, clusterValues)
					if err != nil {
						return fmt.Errorf("retrieving dependencies for controller StatefulSet: %w", err)
					}
					stepLogger.V(1).Info("Retrieved dependencies")

					if err = r.AdvancedStatefulSet.Reconcile(stepCtx, cluster, &desired, deps...); err != nil {
						return fmt.Errorf("reconciling controller StatefulSet: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Controller DaemonSet",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling DaemonSet")

					desired := controller.RenderPlaceholderDaemonSet(
						clusterValues.Namespace,
						clusterValues.Name,
						clusterValues.NodeFilters,
						&clusterValues.NodeController,
					)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered DaemonSet")

					deps, err := r.getControllersDaemonSetDependencies(stepCtx, clusterValues)
					if err != nil {
						return fmt.Errorf("retrieving dependencies for controller DaemonSet: %w", err)
					}
					stepLogger.V(1).Info("Retrieved dependencies for DaemonSet")

					if err = r.DaemonSet.Reconcile(stepCtx, cluster, &desired, deps...); err != nil {
						return fmt.Errorf("reconciling controller DaemonSet: %w", err)
					}
					stepLogger.V(1).Info("Reconciled DaemonSet")

					return nil
				},
			},
		)
	}

	if err := reconcileControllersImpl(); err != nil {
		return fmt.Errorf("reconciling Slurm Controllers: %w", err)
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
	const requeueDuration = 10 * time.Second
	var (
		res = ctrl.Result{}
	)

	logger := log.FromContext(ctx)

	existing := &kruisev1b1.StatefulSet{}
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
			return ctrl.Result{RequeueAfter: requeueDuration}, nil
		}
		logger.Error(err, "Failed to get controller StatefulSet")
		return res, fmt.Errorf("getting controller StatefulSet: %w", err)
	}

	if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) bool {
		var (
			condition metav1.Condition
		)
		if existing.Status.ReadyReplicas == clusterValues.NodeController.StatefulSet.Replicas {
			condition = metav1.Condition{
				Type:    slurmv1.ConditionClusterControllersAvailable,
				Status:  metav1.ConditionTrue,
				Reason:  "Available",
				Message: "Slurm controllers are available",
			}
		} else {
			condition = metav1.Condition{
				Type:    slurmv1.ConditionClusterControllersAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "NotAvailable",
				Message: "Slurm controllers are not available yet",
			}
			res.RequeueAfter += requeueDuration
		}
		return status.SetCondition(condition)
	}); err != nil {
		logger.Error(err, "Failed to update status")
		return res, fmt.Errorf("updating .Status: %w", err)
	}

	return res, nil
}

func (r SlurmClusterReconciler) getControllersStatefulSetDependencies(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
) ([]metav1.Object, error) {
	var res []metav1.Object

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

	if clusterValues.NodeAccounting.Enabled {
		slurmdbdSecret := &corev1.Secret{}
		if err := r.Get(
			ctx,
			types.NamespacedName{
				Namespace: clusterValues.Namespace,
				Name:      naming.BuildSecretSlurmdbdConfigsName(clusterValues.Name),
			},
			slurmdbdSecret,
		); err != nil {
			return []metav1.Object{}, err
		}
		res = append(res, slurmdbdSecret)
	}

	return res, nil
}

// getControllersDaemonSetDependencies returns the dependencies required for the controller DaemonSet.
func (r SlurmClusterReconciler) getControllersDaemonSetDependencies(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
) ([]metav1.Object, error) {
	var res []metav1.Object

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

	if clusterValues.NodeAccounting.Enabled {
		slurmdbdSecret := &corev1.Secret{}
		if err := r.Get(
			ctx,
			types.NamespacedName{
				Namespace: clusterValues.Namespace,
				Name:      naming.BuildSecretSlurmdbdConfigsName(clusterValues.Name),
			},
			slurmdbdSecret,
		); err != nil {
			return []metav1.Object{}, err
		}
		res = append(res, slurmdbdSecret)
	}

	return res, nil
}
