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
		desired, err := benchmark.RenderNCCLBenchmarkCronJob(
			clusterValues.Namespace,
			clusterValues.Name,
			clusterValues.NodeFilters,
			&clusterValues.Secrets,
			clusterValues.VolumeSources,
			&clusterValues.NCCLBenchmark,
		)
		if err != nil {
			logger.Error(err, "Failed to render NCCL benchmark CronJob")
			return errors.Wrap(err, "rendering NCCL benchmark CronJob")
		}
		logger = logger.WithValues(logfield.ResourceKV(&desired)...)

		deps, err := r.getNCCLBenchmarkDependencies(ctx, clusterValues)
		if err != nil {
			logger.Error(err, "Failed to retrieve dependencies for NCCLBenchmark CronJob")
			return errors.Wrap(err, "retrieving dependencies for NCCLBenchmark CronJob")
		}

		err = r.CronJob.Reconcile(ctx, cluster, &desired, deps...)
		if err != nil {
			logger.Error(err, "Failed to reconcile NCCL benchmark CronJob")
			return errors.Wrap(err, "reconciling NCCL benchmark CronJob")
		}
		return nil
	}

	if err := reconcileNCCLBenchmarkImpl(); err != nil {
		logger.Error(err, "Failed to reconcile NCCL benchmark")
		return errors.Wrap(err, "reconciling NCCL benchmark")
	}
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
			Name:      clusterValues.Secrets.MungeKey.Name,
		},
		mungeKeySecret,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, mungeKeySecret)

	return res, nil
}
