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
				Name: "Slurm Login SSH configs ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired, err := login.RenderConfigMapSSHConfigs(clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering login SSH configs ConfigMap")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err = r.ConfigMap.Reconcile(ctx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling login SSH configs ConfigMap")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Login SSH root public keys ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired, err := login.RenderSshRootPublicKeysConfig(clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering login SshRootPublicKeys ConfigMap")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err = r.ConfigMap.Reconcile(ctx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling login SshRootPublicKeys ConfigMap")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Login sshd keys Secret",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired := corev1.Secret{}
					if getErr := r.Get(
						ctx,
						types.NamespacedName{
							Namespace: clusterValues.Namespace,
							Name:      clusterValues.Secrets.SshdKeysName,
						},
						&desired,
					); getErr != nil {
						if !apierrors.IsNotFound(getErr) {
							stepLogger.Error(getErr, "Failed to get")
							return errors.Wrap(getErr, "getting login SSHDKeys Secrets")
						}

						renderedDesired, err := login.RenderSSHDKeysSecret(clusterValues.Name, clusterValues.Namespace, clusterValues.Secrets.SshdKeysName)
						if err != nil {
							stepLogger.Error(err, "Failed to render")
							return errors.Wrap(err, "rendering login SSHDKeys Secrets")
						}
						desired = *renderedDesired.DeepCopy()
						stepLogger.Info("Rendered")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)

					if err := r.Secret.Reconcile(ctx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling login SSHDKeys Secrets")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Login Security limits ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired := common.RenderConfigMapSecurityLimits(consts.ComponentTypeLogin, clusterValues)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err := r.ConfigMap.Reconcile(ctx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling login security limits configmap")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Login Service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired := login.RenderService(clusterValues.Namespace, clusterValues.Name, &clusterValues.NodeLogin)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err := r.Service.Reconcile(ctx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling login Service")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Login StatefulSet",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired, err := login.RenderStatefulSet(
						clusterValues.Namespace,
						clusterValues.Name,
						clusterValues.NodeFilters,
						&clusterValues.Secrets,
						clusterValues.VolumeSources,
						&clusterValues.NodeLogin,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering login StatefulSet")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					deps, err := r.getLoginStatefulSetDependencies(ctx, clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to retrieve dependencies")
						return errors.Wrap(err, "retrieving dependencies for login StatefulSet")
					}
					stepLogger.Info("Retrieved dependencies")

					if err = r.StatefulSet.Reconcile(ctx, cluster, &desired, deps...); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling login StatefulSet")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
		)
	}

	if err := reconcileLoginImpl(); err != nil {
		logger.Error(err, "Failed to reconcile Slurm Login")
		return errors.Wrap(err, "reconciling Slurm Login")
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

	existing := &appsv1.StatefulSet{}
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
		return ctrl.Result{}, errors.Wrap(err, "getting login StatefulSet")
	}

	targetReplicas := clusterValues.NodeLogin.StatefulSet.Replicas
	if existing.Spec.Replicas != nil {
		targetReplicas = *existing.Spec.Replicas
	}
	if existing.Status.AvailableReplicas != targetReplicas {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterLoginAvailable,
			Status: metav1.ConditionFalse, Reason: "NotAvailable",
			Message: "Slurm login is not available yet",
		})
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterLoginAvailable,
			Status: metav1.ConditionTrue, Reason: "Available",
			Message: "Slurm login is available",
		})
	}

	return ctrl.Result{}, nil
}

func (r SlurmClusterReconciler) getLoginStatefulSetDependencies(
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
			Name:      naming.BuildConfigMapSSHConfigsName(clusterValues.Name),
		},
		sshConfigsConfigMap,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, sshConfigsConfigMap)

	return res, nil
}
