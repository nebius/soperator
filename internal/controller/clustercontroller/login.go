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
	"nebius.ai/slurm-operator/internal/render/login"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// ReconcileLogin reconciles all resources necessary for deploying Slurm login
func (r SlurmClusterReconciler) ReconcileLogin(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) error {
	logger := log.FromContext(ctx)

	reconcileLoginImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of Slurm Login",
			utils.MultiStepExecutionStrategyCollectErrors,
			utils.MultiStepExecutionStep{
				Name: "Slurm Login SSHD ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					if !clusterValues.NodeLogin.IsSSHDConfigMapDefault {
						stepLogger.V(1).Info("Use custom SSHD ConfigMap from reference")
						stepLogger.V(1).Info("Reconciled")
						return nil
					}

					desired := login.RenderConfigMapSSHDConfigs(clusterValues, consts.ComponentTypeLogin)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling login default SSHD ConfigMap: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Login SSH root public keys ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := login.RenderSshRootPublicKeysConfig(clusterValues)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling login SSHRootPublicKeys ConfigMap: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Login sshd keys Secret",
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
							return fmt.Errorf("getting login SSHDKeys Secrets: %w", getErr)
						}

						renderedDesired, err := common.RenderSSHDKeysSecret(
							clusterValues.Name,
							clusterValues.Namespace,
							clusterValues.Secrets.SshdKeysName,
							consts.ComponentTypeLogin,
						)
						if err != nil {
							stepLogger.Error(err, "Failed to render")
							return fmt.Errorf("rendering login SSHDKeys Secrets: %w", err)
						}
						desired = *renderedDesired.DeepCopy()
						stepLogger.V(1).Info("Rendered")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)

					if err := r.Secret.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling login SSHDKeys Secrets: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Login Security limits ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := common.RenderConfigMapSecurityLimits(consts.ComponentTypeLogin, clusterValues)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling login security limits configmap: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Login Service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := login.RenderService(clusterValues.Namespace, clusterValues.Name, &clusterValues.NodeLogin)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					var loginNamePtr *string = nil
					if err := r.Service.Reconcile(stepCtx, cluster, &desired, loginNamePtr); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling login Service: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Login Headless Service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := login.RenderHeadlessService(clusterValues.Namespace, clusterValues.Name, &clusterValues.NodeLogin)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					var loginHeadlessNamePtr *string = nil
					if err := r.Service.Reconcile(stepCtx, cluster, &desired, loginHeadlessNamePtr); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling login Headless Service: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Login StatefulSet",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired, err := login.RenderStatefulSet(
						clusterValues.Namespace,
						clusterValues.Name,
						clusterValues.ClusterType,
						clusterValues.NodeFilters,
						&clusterValues.Secrets,
						clusterValues.VolumeSources,
						&clusterValues.NodeLogin,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return fmt.Errorf("rendering login StatefulSet: %w", err)
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					deps, err := r.getLoginStatefulSetDependencies(stepCtx, clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to retrieve dependencies")
						return fmt.Errorf("retrieving dependencies for login StatefulSet: %w", err)
					}
					stepLogger.V(1).Info("Retrieved dependencies")

					if err = r.AdvancedStatefulSet.Reconcile(stepCtx, cluster, &desired, deps...); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling login StatefulSet: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
		)
	}

	if err := reconcileLoginImpl(); err != nil {
		logger.Error(err, "Failed to reconcile Slurm Login")
		return fmt.Errorf("reconciling Slurm Login: %w", err)
	}
	logger.Info("Reconciled Slurm Login")
	return nil
}

// ValidateLogin checks that Slurm login are reconciled with the desired state correctly
func (r SlurmClusterReconciler) ValidateLogin(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	existing := &kruisev1b1.StatefulSet{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      clusterValues.NodeLogin.StatefulSet.Name,
		},
		existing,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
		}
		logger.Error(err, "Failed to get login StatefulSet")
		return ctrl.Result{}, fmt.Errorf("getting login StatefulSet: %w", err)
	}

	targetReplicas := clusterValues.NodeLogin.StatefulSet.Replicas
	if existing.Spec.Replicas != nil {
		targetReplicas = *existing.Spec.Replicas
	}
	if existing.Status.AvailableReplicas != targetReplicas {
		if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
			status.SetCondition(metav1.Condition{
				Type:   slurmv1.ConditionClusterLoginAvailable,
				Status: metav1.ConditionFalse, Reason: "NotAvailable",
				Message: "Slurm login is not available yet",
			})
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
			status.SetCondition(metav1.Condition{
				Type:   slurmv1.ConditionClusterLoginAvailable,
				Status: metav1.ConditionTrue, Reason: "Available",
				Message: "Slurm login is available",
			})
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r SlurmClusterReconciler) getLoginStatefulSetDependencies(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
) ([]metav1.Object, error) {
	var res []metav1.Object

	rootPublicKeys := &corev1.ConfigMap{}
	if err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      naming.BuildConfigMapSshRootPublicKeysName(clusterValues.Name),
		},
		rootPublicKeys,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, rootPublicKeys)

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

	sshConfigsConfigMap := &corev1.ConfigMap{}
	if err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      naming.BuildConfigMapSSHDConfigsNameLogin(clusterValues.Name),
		},
		sshConfigsConfigMap,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, sshConfigsConfigMap)

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

	if clusterValues.NodeLogin.SSHDConfigMapName != "" {
		sshdConfigMap := &corev1.ConfigMap{}
		err := r.Get(
			ctx,
			types.NamespacedName{
				Namespace: clusterValues.Namespace,
				Name:      clusterValues.NodeLogin.SSHDConfigMapName,
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
