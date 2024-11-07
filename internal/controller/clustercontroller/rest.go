package clustercontroller

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/rest"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func (r SlurmClusterReconciler) ReconcileREST(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) error {
	logger := log.FromContext(ctx)

	isRESTEnabled := clusterValues.NodeRest.Enabled
	isAccountingEnabled := clusterValues.NodeAccounting.Enabled
	isExternalDBEnabled := clusterValues.NodeAccounting.ExternalDB.Enabled
	isMariaDBEnabled := clusterValues.NodeAccounting.MariaDb.Enabled

	if !isRESTEnabled {
		logger.Info("Slurm REST API is disabled. Skipping reconciliation")
		return nil
	}

	if !isAccountingEnabled || (!isExternalDBEnabled && !isMariaDBEnabled) {
		logger.Info("Slurm Accounting is disabled. Skipping REST API reconciliation")
		return nil
	}

	reconcileRESTImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of REST API resources",
			utils.MultiStepExecutionStrategyCollectErrors,
			utils.MultiStepExecutionStep{
				Name: "Slurm REST API Service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")
					desired, err := rest.RenderService(
						clusterValues.Namespace,
						clusterValues.Name,
						&clusterValues.NodeRest,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering REST API service")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.Info("Rendered")

					if err = r.Service.Reconcile(stepCtx, cluster, desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling REST API service")
					}
					stepLogger.Info("Reconciled")
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "REST API",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired, err := rest.RenderDeploymentREST(
						clusterValues.Name,
						clusterValues.Namespace,
						&clusterValues.NodeRest,
						clusterValues.NodeFilters,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering ConfigMap with Slurm configs")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.Info("Rendered")

					deps, err := r.getRESTDeploymentDependencies(ctx, clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to retrieve dependencies")
						return errors.Wrap(err, "retrieving dependencies for REST API Deployment")
					}
					stepLogger.Info("Retrieved dependencies")

					if err = r.Deployment.Reconcile(stepCtx, cluster, desired, deps...); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling REST API Deployment")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
		)
	}

	if err := reconcileRESTImpl(); err != nil {
		logger.Error(err, "Failed to reconcile REST resources")
		return errors.Wrap(err, "reconciling REST resources")
	}
	logger.Info("Reconciled REST resources")
	return nil
}

func (r SlurmClusterReconciler) getRESTDeploymentDependencies(
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
			name: naming.BuildSecretSlurmRESTSecretName(clusterValues.Name),
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
