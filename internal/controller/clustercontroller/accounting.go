package clustercontroller

import (
	"context"
	"fmt"
	"time"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
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
	isMariaDBEnabled := clusterValues.NodeAccounting.MariaDb.Enabled
	isProtectedSecret := clusterValues.NodeAccounting.MariaDb.ProtectedSecret

	reconcileAccountingImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of Accounting",
			utils.MultiStepExecutionStrategyCollectErrors,
			utils.MultiStepExecutionStep{
				Name: "Slurm Secret Slurmdbd Configs",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					if !isAccountingEnabled || (!isExternalDBEnabled && !isMariaDBEnabled) {
						logger.Info("Slurm Accounting is disabled. Skipping reconciliation	of Slurmdbd Configs Secret")
						return nil
					}

					var secret = &corev1.Secret{}
					var err error

					if isExternalDBEnabled {
						err = r.handleExternalDB(stepCtx, clusterValues, secret)
						if err != nil {
							return err
						}
					}

					if isMariaDBEnabled {
						err = r.handleMariaDB(stepCtx, clusterValues, consts.MariaDbSecretName, secret)
						if err != nil {
							return err
						}
					}

					desired, err := accounting.RenderSecret(
						clusterValues.Namespace,
						clusterValues.Name,
						&clusterValues.NodeAccounting,
						secret,
						clusterValues.NodeRest.Enabled,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering accounting Secret")
					}

					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.Info("Rendered")

					if err = r.Secret.Reconcile(stepCtx, cluster, desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling accounting Secret")
					}

					stepLogger.Info("Reconciled")
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Secret MariaDB password",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					if !isAccountingEnabled || (!isExternalDBEnabled && !isMariaDBEnabled) {
						logger.Info("Slurm Accounting is disabled. Skipping reconciliation of MariaDB password Secret")
						return nil
					}

					if !isMariaDBEnabled || !isProtectedSecret {
						stepLogger.Info("Reconciled")
						return nil
					}

					var secret = &corev1.Secret{}

					err := r.handleMariaDB(stepCtx, clusterValues, consts.MariaDbSecretName, secret)

					if apierrors.IsNotFound(err) {
						desired, err := accounting.RenderSecretMariaDb(
							clusterValues.Namespace,
							consts.MariaDbSecretName,
							clusterValues.Name,
						)
						if err != nil {
							stepLogger.Error(err, "Failed to render")
							return errors.Wrap(err, "rendering mariadb password Secret")
						}
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
						stepLogger.Info("Rendered")

						err = r.Create(ctx, desired)
						if err != nil {
							stepLogger.Error(err, "Failed to create")
							return errors.Wrap(err, "creating mariadb password Secret")
						}
					}
					return err
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Secret MariaDB root password",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					if !isAccountingEnabled || (!isExternalDBEnabled && !isMariaDBEnabled) {
						logger.Info("Slurm Accounting is disabled. Skipping reconciliation MariaDB root password")
						return nil
					}

					if !isMariaDBEnabled || !isProtectedSecret {
						stepLogger.Info("Reconciled")
						return nil
					}

					var secret = &corev1.Secret{}

					err := r.handleMariaDB(stepCtx, clusterValues, consts.MariaDbSecretRootName, secret)

					if apierrors.IsNotFound(err) {
						desired, err := accounting.RenderSecretMariaDb(
							clusterValues.Namespace,
							consts.MariaDbSecretRootName,
							clusterValues.Name,
						)
						if err != nil {
							stepLogger.Error(err, "Failed to render")
							return errors.Wrap(err, "rendering mariadb root password Secret")
						}
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
						stepLogger.Info("Rendered")

						err = r.Create(ctx, desired)
						if err != nil {
							stepLogger.Error(err, "Failed to create")
							return errors.Wrap(err, "creating mariadb root password Secret")
						}
					}
					return err
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm accounting service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					var err error
					var desired *corev1.Service
					var accountingServiceNamePtr *string = nil

					if !isAccountingEnabled {
						stepLogger.Info("Removing")
						accountingServiceName := naming.BuildServiceName(consts.ComponentTypeAccounting, clusterValues.Name)
						accountingServiceNamePtr = &accountingServiceName

						if err = r.Service.Reconcile(stepCtx, cluster, desired, accountingServiceNamePtr); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return errors.Wrap(err, "reconciling accounting service")
						}
						stepLogger.Info("Reconciled")
						return nil
					}

					desired, err = accounting.RenderService(
						clusterValues.Namespace,
						clusterValues.Name,
						&clusterValues.NodeAccounting,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering accounting Service")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.Info("Rendered")

					if err = r.Service.Reconcile(stepCtx, cluster, desired, accountingServiceNamePtr); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling accounting service")
					}
					stepLogger.Info("Reconciled")
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Deployment",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					var err error
					var desired *appsv1.Deployment
					var deploymentNamePtr *string = nil

					if !isAccountingEnabled {
						stepLogger.Info("Removing")
						deploymentName := naming.BuildDeploymentName(consts.ComponentTypeAccounting)
						deploymentNamePtr = &deploymentName
						if err = r.Deployment.Reconcile(stepCtx, cluster, desired, deploymentNamePtr); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return errors.Wrap(err, "reconciling accounting Deployment")
						}
						stepLogger.Info("Reconciled")
						return nil
					}
					desired, err = accounting.RenderDeployment(
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

					if err = r.Deployment.Reconcile(stepCtx, cluster, desired, deploymentNamePtr, deps...); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling accounting Deployment")
					}
					stepLogger.Info("Reconciled")
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm MariaDB Database",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					if !check.IsMariaDbOperatorCRDInstalled {
						stepLogger.Info("MariaDB Operator CRD is not installed. Skipping MariaDB reconciliation")
						return nil
					}

					var err error
					var desired *mariadbv1alpha1.MariaDB
					var mariaDbNamePtr *string = nil

					if !isMariaDBEnabled || !isAccountingEnabled {
						stepLogger.Info("Removing")
						mariaDbName := naming.BuildMariaDbName(clusterValues.Name)
						mariaDbNamePtr = &mariaDbName
						if err = r.MariaDb.Reconcile(stepCtx, cluster, desired, mariaDbNamePtr); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return errors.Wrap(err, "reconciling accounting mariadb")
						}
						stepLogger.Info("Reconciled")
						return nil
					}

					desired, err = accounting.RenderMariaDb(
						clusterValues.Namespace,
						clusterValues.Name,
						&clusterValues.NodeAccounting,
						clusterValues.NodeFilters,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering accounting mariadb")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.Info("Rendered")

					if err = r.MariaDb.Reconcile(ctx, cluster, desired, mariaDbNamePtr); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling accounting mariadb")
					}
					stepLogger.Info("Reconciled")
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm MariaDB Grant",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					if !check.IsMariaDbOperatorCRDInstalled {
						stepLogger.Info("MariaDB Operator CRD is not installed. Skipping MariaDB reconciliation")
						return nil
					}

					var err error
					var desired *mariadbv1alpha1.Grant
					var mariaDbGrantNamePtr *string = nil

					if !isMariaDBEnabled || !isAccountingEnabled {
						stepLogger.Info("Removing")
						mariaDbGrantName := naming.BuildMariaDbName(clusterValues.Name)
						mariaDbGrantNamePtr = &mariaDbGrantName
						if err = r.MariaDbGrant.Reconcile(stepCtx, cluster, desired, mariaDbGrantNamePtr); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return errors.Wrap(err, "reconciling accounting mariadb grant")
						}
						stepLogger.Info("Reconciled")
						return nil
					}

					desired, err = accounting.RenderMariaDbGrant(
						clusterValues.Namespace,
						clusterValues.Name,
						&clusterValues.NodeAccounting,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering accounting mariadb grant")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.Info("Rendered")

					if err = r.MariaDbGrant.Reconcile(ctx, cluster, desired, mariaDbGrantNamePtr); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling accounting mariadb grant")
					}
					stepLogger.Info("Reconciled")
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

func (r SlurmClusterReconciler) handleExternalDB(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	secret *corev1.Secret) error {
	logger := log.FromContext(ctx)

	isSecretNameEmpty := clusterValues.NodeAccounting.ExternalDB.PasswordSecretKeyRef.Name == ""
	if isSecretNameEmpty {
		logger.Error(nil, "Secret name is empty")
		return errors.New("secret name is empty")
	}

	secretNameAcc := clusterValues.NodeAccounting.ExternalDB.PasswordSecretKeyRef.Name
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      secretNameAcc,
		},
		secret,
	)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to get Secret %s", secretNameAcc))
		return errors.Wrap(err, fmt.Sprintf("getting Secret %s", secretNameAcc))
	}

	return nil
}

func (r SlurmClusterReconciler) handleMariaDB(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	secretName string,
	secret *corev1.Secret) error {
	logger := log.FromContext(ctx)

	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      secretName,
		},
		secret,
	)
	if err != nil {
		logger.Error(err, "Failed to get Secret")
		return errors.Wrap(err, "getting Secret")
	}

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
		if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
			status.SetCondition(metav1.Condition{
				Type:   slurmv1.ConditionClusterAccountingAvailable,
				Status: metav1.ConditionFalse, Reason: "NotAvailable",
				Message: "Slurm accounting is not available yet",
			})
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
			status.SetCondition(metav1.Condition{
				Type:   slurmv1.ConditionClusterAccountingAvailable,
				Status: metav1.ConditionTrue, Reason: "Available",
				Message: "Slurm accounting is available",
			})
		}); err != nil {
			return ctrl.Result{}, err
		}
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
