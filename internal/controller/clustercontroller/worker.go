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
					stepLogger.V(1).Info("Reconciling")

					desired, err := worker.RenderConfigMapNCCLTopology(clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering worker NCCL topology ConfigMap")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err = r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker NCCL topology ConfigMap")
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker sysctl ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired, err := worker.RenderConfigMapSysctl(clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering worker Sysctl ConfigMap")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err = r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker Sysctl ConfigMap")
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker SSHD ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					if !clusterValues.NodeWorker.IsSSHDConfigMapDefault {
						stepLogger.V(1).Info("Use custom SSHD ConfigMap from reference")
						stepLogger.V(1).Info("Reconciled")
						return nil
					}

					desired, err := common.RenderDefaultConfigMapSSHDConfigs(clusterValues, consts.ComponentTypeWorker)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering worker default SSHD ConfigMap")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err = r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker default SSHD ConfigMap")
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker sshd keys Secret",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := corev1.Secret{}
					if getErr := r.Get(
						stepCtx,
						types.NamespacedName{
							Namespace: clusterValues.Namespace,
							Name:      clusterValues.Secrets.SshdKeysName,
						},
						&desired,
					); getErr != nil {
						if !apierrors.IsNotFound(getErr) {
							stepLogger.Error(getErr, "Failed to get")
							return errors.Wrap(getErr, "getting worker SSHDKeys Secrets")
						}

						renderedDesired, err := common.RenderSSHDKeysSecret(
							clusterValues.Name,
							clusterValues.Namespace,
							clusterValues.Secrets.SshdKeysName,
							consts.ComponentTypeWorker,
						)
						if err != nil {
							stepLogger.Error(err, "Failed to render")
							return errors.Wrap(err, "rendering worker SSHDKeys Secrets")
						}
						desired = *renderedDesired.DeepCopy()
						stepLogger.V(1).Info("Rendered")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)

					if err := r.Secret.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker SSHDKeys Secrets")
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker Security limits ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := common.RenderConfigMapSecurityLimits(consts.ComponentTypeWorker, clusterValues)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker security limits configmap")
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker Supervisord ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					generateDefault := clusterValues.NodeWorker.SupervisordConfigMapDefault
					if generateDefault {
						stepLogger.V(1).Info("Generate default ConfigMap")
						desired := worker.RenderDefaultConfigMapSupervisord(clusterValues)
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
						stepLogger.V(1).Info("Rendered")

						if err := r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return errors.Wrap(err, "reconciling worker supervisord configmap")
						}
					} else {
						stepLogger.V(1).Info("Use custom ConfigMap")
					}

					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker Service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := worker.RenderService(clusterValues.Namespace, clusterValues.Name, &clusterValues.NodeWorker)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.Service.Reconcile(stepCtx, cluster, &desired, nil); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker Service")
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker StatefulSet",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired, err := worker.RenderStatefulSet(
						clusterValues.Namespace,
						clusterValues.Name,
						clusterValues.ClusterType,
						clusterValues.NodeFilters,
						&clusterValues.Secrets,
						clusterValues.VolumeSources,
						&clusterValues.NodeWorker,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering worker StatefulSet")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					deps, err := r.getWorkersStatefulSetDependencies(stepCtx, clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to retrieve dependencies")
						return errors.Wrap(err, "retrieving dependencies for worker StatefulSet")
					}
					stepLogger.V(1).Info("Retrieved dependencies")

					if err = r.StatefulSet.Reconcile(stepCtx, cluster, &desired, deps...); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker StatefulSet")
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker ServiceAccount",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := worker.RenderServiceAccount(clusterValues.Namespace, clusterValues.Name)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.ServiceAccount.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling worker ServiceAccount")
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker Role",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					if clusterValues.Telemetry != nil && clusterValues.Telemetry.JobsTelemetry != nil && clusterValues.Telemetry.JobsTelemetry.SendJobsEvents {
						desired := worker.RenderRole(clusterValues.Namespace, clusterValues.Name)
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
						stepLogger.V(1).Info("Rendered")

						if err := r.Role.Reconcile(stepCtx, cluster, &desired); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return errors.Wrap(err, "reconciling worker Role")
						}
					} else {
						// If SendJobsEvents is set to false or nil, the Role is not necessary because we don't need the permissions.
						// We need to explicitly delete the Role in case the user initially set JobsEvents to true and then removed it or set it to false.
						// Without explicit deletion through reconciliation,
						// The Role will not be deleted, leading to inconsistency between what is specified in the SlurmCluster kind and the actual state in the cluster.
						stepLogger.V(1).Info("Removing")
						if err := r.Role.Reconcile(stepCtx, cluster, nil); err != nil {
							stepLogger.Error(err, "Failed to remove")
							return errors.Wrap(err, "removing worker Role")
						}
						stepLogger.V(1).Info("Removed")
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Worker RoleBinding",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					if clusterValues.Telemetry != nil && clusterValues.Telemetry.JobsTelemetry != nil && clusterValues.Telemetry.JobsTelemetry.SendJobsEvents {
						desired := worker.RenderRoleBinding(clusterValues.Namespace, clusterValues.Name)
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
						stepLogger.V(1).Info("Rendered")

						if err := r.RoleBinding.Reconcile(stepCtx, cluster, &desired); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return errors.Wrap(err, "reconciling worker RoleBinding")
						}
					} else {
						// If SendJobsEvents is set to false or nil, the RoleBinding is not necessary because we don't need the permissions.
						// We need to explicitly delete the RoleBinding in case the user initially set JobsEvents to true and then removed it or set it to false.
						// Without explicit deletion through reconciliation,
						// Еhe RoleBinding will not be deleted, leading to inconsistency between what is specified in the SlurmCluster kind and the actual state in the cluster.
						stepLogger.V(1).Info("Removing")
						if err := r.RoleBinding.Reconcile(stepCtx, cluster, nil); err != nil {
							stepLogger.Error(err, "Failed to remove")
							return errors.Wrap(err, "removing worker Role")
						}
						stepLogger.V(1).Info("Removed")
					}
					stepLogger.V(1).Info("Reconciled")

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

	if clusterValues.NodeWorker.SupervisordConfigMapName != "" {
		superviserdConfigMap := &corev1.ConfigMap{}
		err := r.Get(
			ctx,
			types.NamespacedName{
				Namespace: clusterValues.Namespace,
				Name:      clusterValues.NodeWorker.SupervisordConfigMapName,
			},
			superviserdConfigMap,
		)
		if err != nil {
			return []metav1.Object{}, err
		}
		res = append(res, superviserdConfigMap)
	}

	if clusterValues.NodeWorker.SSHDConfigMapName != "" {
		sshdConfigMap := &corev1.ConfigMap{}
		err := r.Get(
			ctx,
			types.NamespacedName{
				Namespace: clusterValues.Namespace,
				Name:      clusterValues.NodeWorker.SSHDConfigMapName,
			},
			sshdConfigMap,
		)
		if err != nil {
			return []metav1.Object{}, err
		}
		res = append(res, sshdConfigMap)
	}

	return res, nil
}
