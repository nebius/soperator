package clustercontroller

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/login"
	"nebius.ai/slurm-operator/internal/values"
)

// DeployLogin creates all resources necessary for deploying Slurm login
func (r SlurmClusterReconciler) DeployLogin(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	clusterCR *slurmv1.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Deploy SSH configs ConfigMap
	{
		found := &corev1.ConfigMap{}
		dep, err := login.RenderConfigMapSSHConfigs(clusterValues)
		if err != nil {
			return ctrl.Result{}, err
		}
		if res, err := r.EnsureDeployed(ctx, &dep, found, clusterCR); err != nil {
			return res, err
		}
	}

	// Deploy Service
	{
		found := &corev1.Service{}
		dep := login.RenderService(clusterValues.Namespace, clusterValues.Name, &clusterValues.NodeLogin)
		if res, err := r.EnsureDeployed(ctx, &dep, found, clusterCR); err != nil {
			return res, err
		}
	}

	// Deploy StatefulSet
	{
		found := &appsv1.StatefulSet{}
		dep, err := login.RenderStatefulSet(
			clusterValues.Namespace,
			clusterValues.Name,
			clusterValues.NodeFilters,
			&clusterValues.Secrets,
			clusterValues.VolumeSources,
			&clusterValues.NodeLogin,
		)
		if err != nil {
			logger.Error(err, "Login StatefulSet deployment failed")
			return ctrl.Result{}, err
		}
		dependencies, err := r.getLoginStatefulSetDependencies(ctx, clusterValues)
		if err != nil {
			logger.Error(err, "Failed at retrieving dependencies for the login StatefulSet")
			return ctrl.Result{}, err
		}
		if res, err := r.EnsureDeployed(ctx, &dep, found, clusterCR, dependencies...); err != nil {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// UpdateLogin makes sure that Slurm login are up-to-date with CRD
func (r SlurmClusterReconciler) UpdateLogin(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	clusterCR *slurmv1.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	existing := &appsv1.StatefulSet{}
	updated, err := login.RenderStatefulSet(
		clusterValues.Namespace,
		clusterValues.Name,
		clusterValues.NodeFilters,
		&clusterValues.Secrets,
		clusterValues.VolumeSources,
		&clusterValues.NodeLogin,
	)
	if err != nil {
		logger.Error(err, "Login StatefulSet update failed")
		return ctrl.Result{}, err
	}

	dependencies, err := r.getLoginStatefulSetDependencies(ctx, clusterValues)
	if err != nil {
		logger.Error(err, "Failed at retrieving dependencies for the login StatefulSet")
		return ctrl.Result{}, err
	}

	if res, err := r.EnsureUpdated(ctx, &updated, existing, clusterCR, dependencies...); err != nil {
		return res, err
	}

	// TODO Update SSH config if CR changed

	return ctrl.Result{}, nil
}

// ValidateLogin checks that Slurm login are reconciled with the desired state correctly
func (r SlurmClusterReconciler) ValidateLogin(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	clusterCR *slurmv1.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	found := &appsv1.StatefulSet{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Name:      clusterValues.NodeLogin.StatefulSet.Name,
			Namespace: clusterValues.Namespace,
		},
		found,
	)
	if err != nil && apierrors.IsNotFound(err) {
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
	} else if err != nil {
		logger.Error(err, "Failed to get login StatefulSet")
		return ctrl.Result{}, err
	}

	targetReplicas := clusterValues.NodeLogin.StatefulSet.Replicas
	if found.Spec.Replicas != nil {
		targetReplicas = *found.Spec.Replicas
	}
	if found.Status.AvailableReplicas != targetReplicas {
		meta.SetStatusCondition(&clusterCR.Status.Conditions, metav1.Condition{
			Type:   slurmv1.ConditionClusterLoginAvailable,
			Status: metav1.ConditionFalse, Reason: "NotAvailable",
			Message: "Slurm login is not available yet",
		})
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		meta.SetStatusCondition(&clusterCR.Status.Conditions, metav1.Condition{
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

	sshConfigsConfigMap := &corev1.ConfigMap{}
	if err := r.GetNamespacedObject(
		ctx,
		clusterValues.Namespace,
		naming.BuildConfigMapSSHConfigsName(clusterValues.Name),
		sshConfigsConfigMap,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, sshConfigsConfigMap)

	rootPublicKeysSecret := &corev1.Secret{}
	if err := r.GetNamespacedObject(
		ctx,
		clusterValues.Secrets.SSHRootPublicKeys.Name,
		clusterValues.Namespace,
		rootPublicKeysSecret,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, rootPublicKeysSecret)

	return res, nil
}
