package clustercontroller

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/benchmark"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// ReconcileNCCLBenchmark reconciles all resources necessary for deploying NCCLBenchmark Cron Job
func (r SlurmClusterReconciler) ReconcileNCCLBenchmark(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) error {
	logger := log.FromContext(ctx)

	reconcileNCCLBenchmarkImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of NCCL benchmark",
			utils.MultiStepExecutionStrategyFailAtFirstError, utils.MultiStepExecutionStep{
				Name: "NCCL benchmark CronJob",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired, err := benchmark.RenderNCCLBenchmarkCronJob(
						clusterValues.Namespace,
						clusterValues.Name,
						clusterValues.NodeFilters,
						&clusterValues.Secrets,
						clusterValues.VolumeSources,
						&clusterValues.NCCLBenchmark,
						clusterValues.Telemetry,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering NCCL benchmark CronJob")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					deps, err := r.getNCCLBenchmarkDependencies(ctx, clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to retrieve dependencies")
						return errors.Wrap(err, "retrieving dependencies for NCCLBenchmark CronJob")
					}
					stepLogger.Info("Retrieved dependencies")

					if err = r.CronJob.Reconcile(ctx, cluster, &desired, deps...); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling NCCL benchmark CronJob")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
		)
	}

	if err := reconcileNCCLBenchmarkImpl(); err != nil {
		logger.Error(err, "Failed to reconcile NCCL benchmark")
		return errors.Wrap(err, "reconciling NCCL benchmark")
	}
	logger.Info("Reconciled NCCL benchmark")
	return nil
}

func (r SlurmClusterReconciler) getNCCLBenchmarkDependencies(
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
