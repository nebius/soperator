package clustercontroller

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/benchmark"
	"nebius.ai/slurm-operator/internal/values"
)

// DeployBenchmark creates benchmark resources for Slurm cluster.
func (r SlurmClusterReconciler) DeployNCCLBenchmark(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	clusterCR *slurmv1.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	found := &batchv1.CronJob{}
	dep, err := benchmark.RenderCronJob(
		clusterValues.Namespace,
		clusterValues.Name,
		clusterValues.NodeFilters,
		&clusterValues.Secrets,
		clusterValues.VolumeSources,
		&clusterValues.NCCLBenchmark,
	)
	if err != nil {
		logger.Error(err, "NCCLBenchmark CronJob deployment failed")
		return ctrl.Result{}, err
	}

	dependencies, err := r.getNCCLBenchmarkDependencies(ctx, clusterValues)
	if err != nil {
		logger.Error(err, "Failed at retrieving dependencies for the NCCLBenchmark CronJob")
		return ctrl.Result{}, err
	}
	if res, err := r.EnsureDeployed(ctx, &dep, found, clusterCR, dependencies...); err != nil {
		return res, err
	}

	return ctrl.Result{}, nil
}

// UpdateNCCLBenchmark makes sure that NCCLBenchmark are up-to-date with CRD
func (r SlurmClusterReconciler) UpdateNCCLBenchmark(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	clusterCR *slurmv1.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	existing := &batchv1.CronJob{}
	updated, err := benchmark.RenderCronJob(
		clusterValues.Namespace,
		clusterValues.Name,
		clusterValues.NodeFilters,
		&clusterValues.Secrets,
		clusterValues.VolumeSources,
		&clusterValues.NCCLBenchmark,
	)
	if err != nil {
		logger.Error(err, "NCCLBenchmark CronJob update failed")
		return ctrl.Result{}, err
	}
	dependencies, err := r.getNCCLBenchmarkDependencies(ctx, clusterValues)
	if err != nil {
		logger.Error(err, "Failed at retrieving dependencies for the NCCLBenchmark CronJob")
		return ctrl.Result{}, err
	}
	if res, err := r.EnsureUpdated(ctx, &updated, existing, clusterCR, dependencies...); err != nil {
		return res, err
	}

	return ctrl.Result{}, nil
}

// ValidateNCCLBenchmark checks that NCCLBenchmark are reconciled with the desired state correctly
func (r SlurmClusterReconciler) ValidateNCCLBenchmark(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	clusterCR *slurmv1.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	found := &batchv1.CronJob{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Name:      consts.ContainerNameNCCLBenchmark,
			Namespace: clusterValues.Namespace,
		},
		found,
	)
	if err != nil && apierrors.IsNotFound(err) {
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
	} else if err != nil {
		logger.Error(err, "Failed to get NCCLBenchmark CronJob")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r SlurmClusterReconciler) getNCCLBenchmarkDependencies(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
) ([]metav1.Object, error) {
	var res []metav1.Object

	slurmConfigsConfigMap := &corev1.ConfigMap{}
	if err := r.GetNamespacedObject(
		ctx,
		clusterValues.Namespace,
		naming.BuildConfigMapSlurmConfigsName(clusterValues.Name),
		slurmConfigsConfigMap,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, slurmConfigsConfigMap)

	mungeKeySecret := &corev1.Secret{}
	if err := r.GetNamespacedObject(
		ctx,
		clusterValues.Namespace,
		clusterValues.Secrets.MungeKey.Name,
		mungeKeySecret,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, mungeKeySecret)

	return res, nil
}
