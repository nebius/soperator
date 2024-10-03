package clustercontroller

import (
	"context"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
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
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/utils"
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
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of Slurm Workers",
			utils.MultiStepExecutionStrategyCollectErrors,
			utils.MultiStepExecutionStep{
				Name: "Slurm Worker NCCL topology ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired, err := worker.RenderConfigMapNCCLTopology(clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering worker NCCL topology ConfigMap")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err = r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker NCCL topology ConfigMap")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker sysctl ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired, err := worker.RenderConfigMapSysctl(clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering worker Sysctl ConfigMap")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err = r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker Sysctl ConfigMap")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker Security limits ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired := common.RenderConfigMapSecurityLimits(consts.ComponentTypeWorker, clusterValues)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err := r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker security limits configmap")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker Service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired := worker.RenderService(clusterValues.Namespace, clusterValues.Name, &clusterValues.NodeWorker)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err := r.Service.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker Service")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker StatefulSet",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired, err := worker.RenderStatefulSet(
						clusterValues.Namespace,
						clusterValues.Name,
						clusterValues.ClusterType,
						clusterValues.NodeFilters,
						clusterValues.VolumeSources,
						&clusterValues.NodeWorker,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering worker StatefulSet")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					deps, err := r.getWorkersStatefulSetDependencies(stepCtx, clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to retrieve dependencies")
						return errors.Wrap(err, "retrieving dependencies for worker StatefulSet")
					}
					stepLogger.Info("Retrieved dependencies")

					if err = r.StatefulSet.Reconcile(stepCtx, cluster, &desired, deps...); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker StatefulSet")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker ServiceAccount",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired := worker.RenderServiceAccount(clusterValues.Namespace, clusterValues.Name)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err := r.ServiceAccount.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker ServiceAccount")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker Role",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					if clusterValues.Telemetry != nil && clusterValues.Telemetry.JobsTelemetry != nil && clusterValues.Telemetry.JobsTelemetry.SendJobsEvents {
						desired := worker.RenderRole(clusterValues.Namespace, clusterValues.Name)
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
						stepLogger.Info("Rendered")

						if err := r.Role.Reconcile(stepCtx, cluster, &desired); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return errors.Wrap(err, "reconciling worker Role")
						}
					} else {
						// If SendJobsEvents is set to false or nil, the Role is not necessary because we don't need the permissions.
						// We need to explicitly delete the Role in case the user initially set JobsEvents to true and then removed it or set it to false.
						// Without explicit deletion through reconciliation,
						// The Role will not be deleted, leading to inconsistency between what is specified in the SlurmCluster kind and the actual state in the cluster.
						stepLogger.Info("Removing")
						if err := r.Role.Reconcile(stepCtx, cluster, nil); err != nil {
							stepLogger.Error(err, "Failed to remove")
							return errors.Wrap(err, "removing worker Role")
						}
						stepLogger.Info("Removed")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker RoleBinding",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					if clusterValues.Telemetry != nil && clusterValues.Telemetry.JobsTelemetry != nil && clusterValues.Telemetry.JobsTelemetry.SendJobsEvents {
						desired := worker.RenderRoleBinding(clusterValues.Namespace, clusterValues.Name)
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
						stepLogger.Info("Rendered")

						if err := r.RoleBinding.Reconcile(stepCtx, cluster, &desired); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return errors.Wrap(err, "reconciling worker RoleBinding")
						}
					} else {
						// If SendJobsEvents is set to false or nil, the RoleBinding is not necessary because we don't need the permissions.
						// We need to explicitly delete the RoleBinding in case the user initially set JobsEvents to true and then removed it or set it to false.
						// Without explicit deletion through reconciliation,
						// Еhe RoleBinding will not be deleted, leading to inconsistency between what is specified in the SlurmCluster kind and the actual state in the cluster.
						stepLogger.Info("Removing")
						if err := r.RoleBinding.Reconcile(stepCtx, cluster, nil); err != nil {
							stepLogger.Error(err, "Failed to remove")
							return errors.Wrap(err, "removing worker Role")
						}
						stepLogger.Info("Removed")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
		)
	}

	if err := reconcileWorkersImpl(); err != nil {
		logger.Error(err, "Failed to reconcile Slurm Workers")
		return errors.Wrap(err, "reconciling Slurm Workers")
	}
	logger.Info("Reconciled Slurm Workers")
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
		if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
			status.SetCondition(metav1.Condition{
				Type:   slurmv1.ConditionClusterWorkersAvailable,
				Status: metav1.ConditionFalse, Reason: "NotAvailable",
				Message: "Slurm workers are not available yet",
			})
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
			status.SetCondition(metav1.Condition{
				Type:   slurmv1.ConditionClusterWorkersAvailable,
				Status: metav1.ConditionTrue, Reason: "Available",
				Message: "Slurm workers are available",
			})
		}); err != nil {
			return ctrl.Result{}, err
		}
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
