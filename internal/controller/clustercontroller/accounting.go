package clustercontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/accounting"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// ReconcileAccounting reconciles all resources necessary for deploying Slurm accounting
func (r SlurmClusterReconciler) ReconcileAccounting(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) error {
	logger := log.FromContext(ctx)
	isAccountingEnabled := clusterValues.NodeAccounting.Enabled
	isExternalDBEnabled := clusterValues.NodeAccounting.ExternalDB.Enabled

	if !isAccountingEnabled || !isExternalDBEnabled {
		logger.Info("Slurm Accounting is disabled. Skipping reconciliation")
		return nil
	}

	reconcileAccountingImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of Accounting",
			utils.MultiStepExecutionStrategyCollectErrors,
			utils.MultiStepExecutionStep{
				Name: "Slurm Secret Slurmdbd Configs",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					var secret = &corev1.Secret{}
					var err error

					isSecretNameEmpty := clusterValues.NodeAccounting.ExternalDB.PasswordSecretKeyRef.Name == ""
					if isSecretNameEmpty {
						stepLogger.Error(err, "Secret name is empty")
						return errors.Wrap(err, "secret name is empty")
					}

					secretNameAcc := clusterValues.NodeAccounting.ExternalDB.PasswordSecretKeyRef.Name
					err = r.Get(
						ctx,
						types.NamespacedName{
							Namespace: clusterValues.Namespace,
							Name:      secretNameAcc,
						},
						secret,
					)
					if err != nil {
						stepLogger.Error(err, fmt.Sprintf("Failed to get Secret %s", secretNameAcc))
						return errors.Wrap(err, fmt.Sprintf("getting Secret %s", secretNameAcc))
					}
					desired, err := accounting.RenderSecret(
						clusterValues.Namespace,
						clusterValues.Name,
						&clusterValues.NodeAccounting,
						secret,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering accounting Secret")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.Info("Rendered")

					if err = r.Secret.Reconcile(ctx, cluster, desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling accounting Secret")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")
					if clusterValues.NodeAccounting.Enabled {
						desired, err := accounting.RenderService(
							clusterValues.Namespace,
							clusterValues.Name,
							clusterValues.NodeAccounting,
						)
						if err != nil {
							stepLogger.Error(err, "Failed to render")
							return errors.Wrap(err, "rendering accounting Service")
						}
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
						stepLogger.Info("Rendered")

						if err = r.Service.Reconcile(ctx, cluster, desired); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return errors.Wrap(err, "reconciling accounting Deployment")
						}
						stepLogger.Info("Reconciled")
					}
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Deployment",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")
					if clusterValues.NodeAccounting.Enabled {
						desired, err := accounting.RenderDeployment(
							clusterValues.Namespace,
							clusterValues.Name,
							&clusterValues.NodeAccounting,
							clusterValues.NodeFilters,
							clusterValues.VolumeSources,
						)
						if err != nil {
							stepLogger.Error(err, "Failed to render")
							return errors.Wrap(err, "rendering accounting Deployment")
						}
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
						stepLogger.Info("Rendered")

						deps, err := r.getAccountingDeploymentDependencies(ctx, clusterValues)
						if err != nil {
							stepLogger.Error(err, "Failed to retrieve dependencies")
							return errors.Wrap(err, "retrieving dependencies for accounting Deployment")
						}
						stepLogger.Info("Retrieved dependencies")

						if err = r.Deployment.Reconcile(ctx, cluster, desired, deps...); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return errors.Wrap(err, "reconciling accounting Deployment")
						}
						stepLogger.Info("Reconciled")
					}
					return nil
				},
			},
		)
	}

	if err := reconcileAccountingImpl(); err != nil {
		logger.Error(err, "Failed to reconcile Slurm Accounting")
		return errors.Wrap(err, "reconciling Slurm Accounting")
	}
	logger.Info("Reconciled Slurm Accounting")
	return nil
}

// ValidateAccounting checks that Slurm accounting are reconciled with the desired state correctly
func (r SlurmClusterReconciler) ValidateAccounting(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	existing := &appsv1.Deployment{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      clusterValues.NodeAccounting.Deployment.Name,
		},
		existing,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
		}
		logger.Error(err, "Failed to get accounting Deployment")
		return ctrl.Result{}, errors.Wrap(err, "getting accounting Deployment")
	}

	targetReplicas := clusterValues.NodeAccounting.Deployment.Replicas
	if existing.Spec.Replicas != nil {
		targetReplicas = *existing.Spec.Replicas
	}
	if existing.Status.AvailableReplicas != targetReplicas {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterAccountingAvailable,
			Status: metav1.ConditionFalse, Reason: "NotAvailable",
			Message: "Slurm accounting are not available yet",
		})
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterAccountingAvailable,
			Status: metav1.ConditionTrue, Reason: "Available",
			Message: "Slurm accounting are available",
		})
	}

	return ctrl.Result{}, nil
}

func (r SlurmClusterReconciler) getAccountingDeploymentDependencies(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
) ([]metav1.Object, error) {
	var res []metav1.Object

	// Define the names and the corresponding objects
	objects := [3]struct {
		name string
		obj  client.Object
	}{
		{
			name: naming.BuildConfigMapSlurmConfigsName(clusterValues.Name),
			obj:  &corev1.ConfigMap{},
		},
		{
			name: naming.BuildSecretSlurmdbdConfigsName(clusterValues.Name),
			obj:  &corev1.Secret{},
		},
		{
			name: naming.BuildSecretMungeKeyName(clusterValues.Name),
			obj:  &corev1.Secret{},
		},
	}

	for _, object := range objects {
		if err := r.Get(
			ctx,
			types.NamespacedName{
				Namespace: clusterValues.Namespace,
				Name:      object.name,
			},
			object.obj,
		); err != nil {
			return nil, err
		}
		res = append(res, object.obj)
	}

	return res, nil
}
