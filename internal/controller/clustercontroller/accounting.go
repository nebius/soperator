package clustercontroller

import (
	"context"
	"errors"
	"fmt"
	"time"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
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
	isExternalDBWithPassword := clusterValues.NodeAccounting.ExternalDB.PasswordSecretKeyRef.Name != ""
	isExternalDBWithClientCert := clusterValues.NodeAccounting.ExternalDB.TLS.ClientCertSecretRef != ""
	isMariaDBEnabled := clusterValues.NodeAccounting.MariaDb.Enabled
	isProtectedSecret := clusterValues.NodeAccounting.MariaDb.ProtectedSecret
	isDBEnabled := isExternalDBEnabled || isMariaDBEnabled

	// Important: this service will restart every time slurm-configs ConfigMap changes
	// We've left this behavior for this service, because it doesn't use Jail, and current realisation require Jail
	//
	reconcileAccountingImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of Accounting",
			utils.MultiStepExecutionStrategyCollectErrors,
			utils.MultiStepExecutionStep{
				Name: "Slurm Secret Slurmdbd Configs",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					if !isAccountingEnabled && !isDBEnabled {
						logger.V(1).Info("Slurm Accounting is disabled. Skipping reconciliation	of Slurmdbd Configs Secret")
						return nil
					}

					var secret *corev1.Secret = nil
					var err error

					if isExternalDBEnabled {
						if isExternalDBWithClientCert {
							err = r.handleExternalDBClientCert(stepCtx, clusterValues)
							if err != nil {
								return err
							}
						}
						if isExternalDBWithPassword {
							secret = &corev1.Secret{}
							err = r.handleExternalDBPassword(stepCtx, clusterValues, secret)
							if err != nil {
								return err
							}
						}
					}

					if isMariaDBEnabled {
						secret = &corev1.Secret{}
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
						return fmt.Errorf("rendering accounting Secret: %w", err)
					}

					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.V(1).Info("Rendered")

					if err = r.Secret.Reconcile(stepCtx, cluster, desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling accounting Secret: %w", err)
					}

					stepLogger.V(1).Info("Reconciled")
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Secret MariaDB password",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					if !isAccountingEnabled && !isDBEnabled {
						logger.V(1).Info("Slurm Accounting is disabled. Skipping reconciliation of MariaDB password Secret")
						return nil
					}

					if !isMariaDBEnabled || !isProtectedSecret {
						stepLogger.V(1).Info("Reconciled")
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
							return fmt.Errorf("rendering mariadb password Secret: %w", err)
						}
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
						stepLogger.V(1).Info("Rendered")

						err = r.Create(ctx, desired)
						if err != nil {
							stepLogger.Error(err, "Failed to create")
							return fmt.Errorf("creating mariadb password Secret: %w", err)
						}
					}
					return err
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Secret MariaDB root password",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					if !isAccountingEnabled && !isDBEnabled {
						logger.V(1).Info("Slurm Accounting is disabled. Skipping reconciliation MariaDB root password")
						return nil
					}

					if !isMariaDBEnabled || !isProtectedSecret {
						stepLogger.V(1).Info("Reconciled")
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
							return fmt.Errorf("rendering mariadb root password Secret: %w", err)
						}
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
						stepLogger.V(1).Info("Rendered")

						err = r.Create(ctx, desired)
						if err != nil {
							stepLogger.Error(err, "Failed to create")
							return fmt.Errorf("creating mariadb root password Secret: %w", err)
						}
					}
					return err
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm accounting service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					var err error
					var desired *corev1.Service
					var accountingServiceNamePtr *string = nil

					if !isAccountingEnabled {
						stepLogger.V(1).Info("Removing")
						accountingServiceName := naming.BuildServiceName(consts.ComponentTypeAccounting, clusterValues.Name)
						accountingServiceNamePtr = &accountingServiceName

						if err = r.Service.Reconcile(stepCtx, cluster, desired, accountingServiceNamePtr); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return fmt.Errorf("reconciling accounting service: %w", err)
						}
						stepLogger.V(1).Info("Reconciled")
						return nil
					}

					desired = accounting.RenderService(
						clusterValues.Namespace,
						clusterValues.Name,
						&clusterValues.NodeAccounting,
					)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.V(1).Info("Rendered")

					if err = r.Service.Reconcile(stepCtx, cluster, desired, accountingServiceNamePtr); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling accounting service: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm MariaDB Grant",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					if !check.IsMariaDbOperatorCRDInstalled {
						stepLogger.V(1).Info("MariaDB Operator CRD is not installed. Skipping MariaDB reconciliation")
						return nil
					}

					var err error
					var desired *mariadbv1alpha1.Grant
					var mariaDbGrantNamePtr *string = nil

					if !isMariaDBEnabled || !isAccountingEnabled {
						stepLogger.V(1).Info("Removing")
						mariaDbGrantName := naming.BuildMariaDbName(clusterValues.Name)
						mariaDbGrantNamePtr = &mariaDbGrantName
						if err = r.MariaDbGrant.Reconcile(stepCtx, cluster, desired, mariaDbGrantNamePtr); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return fmt.Errorf("reconciling accounting mariadb grant: %w", err)
						}
						stepLogger.V(1).Info("Reconciled")
						return nil
					}

					desired, err = accounting.RenderMariaDbGrant(
						clusterValues.Namespace,
						clusterValues.Name,
						&clusterValues.NodeAccounting,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return fmt.Errorf("rendering accounting mariadb grant: %w", err)
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.V(1).Info("Rendered")

					if err = r.MariaDbGrant.Reconcile(ctx, cluster, desired, mariaDbGrantNamePtr); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling accounting mariadb grant: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm MariaDB Database",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					if !check.IsMariaDbOperatorCRDInstalled {
						stepLogger.V(1).Info("MariaDB Operator CRD is not installed. Skipping MariaDB reconciliation")
						return nil
					}

					var err error
					var desired *mariadbv1alpha1.MariaDB
					var mariaDbNamePtr *string = nil

					if !isMariaDBEnabled || !isAccountingEnabled {
						stepLogger.V(1).Info("Removing")
						mariaDbName := naming.BuildMariaDbName(clusterValues.Name)
						mariaDbNamePtr = &mariaDbName
						if err = r.MariaDb.Reconcile(stepCtx, cluster, desired, mariaDbNamePtr); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return fmt.Errorf("reconciling accounting mariadb: %w", err)
						}
						stepLogger.V(1).Info("Reconciled")
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
						return fmt.Errorf("rendering accounting mariadb: %w", err)
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.V(1).Info("Rendered")

					if err = r.MariaDb.Reconcile(ctx, cluster, desired, mariaDbNamePtr); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling accounting mariadb: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Deployment",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					var err error
					var desired *appsv1.Deployment

					if !isAccountingEnabled {
						stepLogger.V(1).Info("Removing")
						deploymentName := naming.BuildDeploymentName(consts.ComponentTypeAccounting)
						if err = r.Deployment.Cleanup(stepCtx, cluster, deploymentName); err != nil {
							return fmt.Errorf("cleanup accounting Deployment: %w", err)
						}
						stepLogger.V(1).Info("Reconciled")
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
						return fmt.Errorf("rendering accounting Deployment: %w", err)
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.V(1).Info("Rendered")

					deps, err := r.getAccountingDeploymentDependencies(ctx, clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to retrieve dependencies")
						return fmt.Errorf("retrieving dependencies for accounting Deployment: %w", err)
					}
					stepLogger.V(1).Info("Retrieved dependencies")

					if err = r.Deployment.Reconcile(stepCtx, cluster, *desired, deps...); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling accounting Deployment: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")
					return nil
				},
			},
		)
	}

	if err := reconcileAccountingImpl(); err != nil {
		logger.Error(err, "Failed to reconcile Slurm Accounting")
		return fmt.Errorf("reconciling Slurm Accounting: %w", err)
	}
	logger.Info("Reconciled Slurm Accounting")
	return nil
}

func (r SlurmClusterReconciler) handleExternalDBClientCert(
	ctx context.Context,
	clusterValues *values.SlurmCluster) error {

	isSecretNameEmpty := clusterValues.NodeAccounting.ExternalDB.TLS.ClientCertSecretRef == ""
	if isSecretNameEmpty {
		return errors.New("client cert secret name is empty")
	}

	secretName := clusterValues.NodeAccounting.ExternalDB.TLS.ClientCertSecretRef
	secret := &corev1.Secret{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      secretName,
		},
		secret,
	)

	if err != nil {
		return err
	}

	if secret.Data == nil {
		return errors.New("client cert secret: data is empty")
	}

	if len(secret.Data[consts.SecretSlurmdbdSSLClientKeyCertificateFile]) == 0 {
		return errors.New("client cert secret: certificate is empty")
	}

	if len(secret.Data[consts.SecretSlurmdbdSSLClientKeyPrivateKeyFile]) == 0 {
		return errors.New("client cert secret: private key is empty")
	}

	return nil
}

func (r SlurmClusterReconciler) handleExternalDBPassword(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	secret *corev1.Secret) error {
	logger := log.FromContext(ctx)

	isSecretNameEmpty := clusterValues.NodeAccounting.ExternalDB.PasswordSecretKeyRef.Name == ""
	if isSecretNameEmpty {
		logger.Error(nil, "Password secret name is empty")
		return errors.New("password secret name is empty")
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
		return fmt.Errorf("getting Secret %s: %w", secretNameAcc, err)
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
		return fmt.Errorf("getting Secret: %w", err)
	}

	return nil
}

// ValidateAccounting checks that Slurm accounting are reconciled with the desired state correctly
func (r SlurmClusterReconciler) ValidateAccounting(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) (ctrl.Result, error) {
	existingDeployment := &appsv1.Deployment{}
	existingMariaDb := &mariadbv1alpha1.MariaDB{}
	existingMariaDbGrant := &mariadbv1alpha1.Grant{}

	existingDeployment, err := getTypedResource(ctx, r.Client, clusterValues.Namespace, clusterValues.NodeAccounting.Deployment.Name, existingDeployment)
	if err != nil {
		message := "Failed to get Deployment"
		return r.updateAccountingAvailabilityStatus(ctx, cluster, metav1.ConditionUnknown, "Unknown", message, 10*time.Second)
	}

	targetReplicas := clusterValues.NodeAccounting.Deployment.Replicas
	if existingDeployment.Spec.Replicas != nil {
		targetReplicas = *existingDeployment.Spec.Replicas
	}

	if clusterValues.NodeAccounting.MariaDb.Enabled {
		existingMariaDb, mariadbErr := getTypedResource(ctx, r.Client, clusterValues.Namespace, naming.BuildMariaDbName(clusterValues.Name), existingMariaDb)
		if mariadbErr != nil || existingMariaDb == nil {
			message := "Failed to get MariaDB"
			return r.updateAccountingAvailabilityStatus(ctx, cluster, metav1.ConditionUnknown, "Unknown", message, 10*time.Second)
		}
		existingMariaDbGrant, mariadbGrantErr := getTypedResource(ctx, r.Client, clusterValues.Namespace, naming.BuildMariaDbName(clusterValues.Name), existingMariaDbGrant)
		if mariadbGrantErr != nil || existingMariaDbGrant == nil {
			message := "Failed to get MariaDB Grant"
			return r.updateAccountingAvailabilityStatus(ctx, cluster, metav1.ConditionUnknown, "Unknown", message, 10*time.Second)
		}
	}

	switch {
	case !isConditionReady(existingMariaDb.Status.Conditions, mariadbv1alpha1.ConditionTypeReady):
		message := "MariaDB is not ready"
		return r.updateAccountingAvailabilityStatus(ctx, cluster, metav1.ConditionFalse, "NotAvailable", message, 10*time.Second)
	case !isConditionReady(existingMariaDbGrant.Status.Conditions, mariadbv1alpha1.ConditionTypeReady):
		message := "MariaDB Grant is not ready"
		return r.updateAccountingAvailabilityStatus(ctx, cluster, metav1.ConditionFalse, "NotAvailable", message, 10*time.Second)
	case existingDeployment.Status.AvailableReplicas == 0:
		message := "Slurm accounting is not available yet"
		return r.updateAccountingAvailabilityStatus(ctx, cluster, metav1.ConditionFalse, "NotAvailable", message, 10*time.Second)
	case existingDeployment.Status.AvailableReplicas != targetReplicas:
		message := fmt.Sprintf("Slurm accounting available replicas: %d, but target replicas: %d", existingDeployment.Status.AvailableReplicas, targetReplicas)
		return r.updateAccountingAvailabilityStatus(ctx, cluster, metav1.ConditionFalse, "NotAvailable", message, 10*time.Second)
	}

	return r.updateAccountingAvailabilityStatus(ctx, cluster, metav1.ConditionTrue, "Available", "Slurm accounting is available", 0)
}

func getTypedResource[T client.Object](
	ctx context.Context,
	getter client.Reader,
	namespace, name string,
	obj T,
) (T, error) {
	err := getter.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			var zeroValue T // This creates the zero value for type T
			return zeroValue, nil
		}
		return obj, fmt.Errorf("failed to get resource: %w", err)
	}
	return obj, nil
}

func (r SlurmClusterReconciler) updateAccountingAvailabilityStatus(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	conditionStatus metav1.ConditionStatus,
	reason, message string,
	requeueAfter time.Duration,
) (ctrl.Result, error) {
	if err := r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) bool {
		return status.SetCondition(metav1.Condition{
			Type:    slurmv1.ConditionClusterAccountingAvailable,
			Status:  conditionStatus,
			Reason:  reason,
			Message: message,
		})
	}); err != nil {
		return ctrl.Result{}, err
	}
	if conditionStatus != metav1.ConditionTrue {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

func isConditionReady(conditions []metav1.Condition, conditionType string) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r SlurmClusterReconciler) getAccountingDeploymentDependencies(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
) ([]metav1.Object, error) {
	var res []metav1.Object

	// Define the names and the corresponding objects
	type accountingDep struct {
		name string
		obj  client.Object
	}
	objects := []accountingDep{
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

	if clusterValues.NodeAccounting.ExternalDB.Enabled {
		externalDBSecretRefs := []string{
			clusterValues.NodeAccounting.ExternalDB.PasswordSecretKeyRef.Name,
			clusterValues.NodeAccounting.ExternalDB.TLS.ServerCASecretRef,
			clusterValues.NodeAccounting.ExternalDB.TLS.ClientCertSecretRef,
		}

		for _, secretRef := range externalDBSecretRefs {
			if secretRef != "" {
				objects = append(objects, accountingDep{
					name: secretRef,
					obj:  &corev1.Secret{},
				})
			}
		}
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
