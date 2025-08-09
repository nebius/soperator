package reconciler

import (
	"context"
	"errors"
	"fmt"
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	otelv1beta1 "github.com/open-telemetry/opentelemetry-operator/apis/v1beta1"
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apparmor "sigs.k8s.io/security-profiles-operator/api/apparmorprofile/v1alpha1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/logfield"
)

// Reconciler is the base type used for reconciler objects
type (
	Reconciler struct {
		client.Client

		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	patchFunc func(existing, desired client.Object) (client.Patch, error)
	patcher   interface {
		patch(existing, desired client.Object) (client.Patch, error)
	}
)

func NewReconciler(client client.Client, scheme *runtime.Scheme, record record.EventRecorder) *Reconciler {
	return &Reconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: record,
	}
}

// EnsureDeployed ensures that `desired` resource is deployed into `owner`. If a corresponding resource `existing` is
// found, it doesn't take any action
func (r Reconciler) EnsureDeployed(
	ctx context.Context,
	owner,
	existing,
	desired client.Object,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx).WithValues(logfield.ResourceKV(desired)...)

	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if err == nil {
		// resource is already deployed
		return nil
	}

	// resource is present, but failed to be gotten
	if !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to get existing resource")
		return fmt.Errorf("getting existing resource: %w", err)
	}

	logger.V(1).Info("Creating new resource")
	{
		if err = ctrl.SetControllerReference(owner, desired, r.Scheme); err != nil {
			logger.Error(err, "Failed to set controller reference")
			return fmt.Errorf("setting controller reference: %w", err)
		}

		err = r.updateDependencyVersions(ctx, desired, deps...)
		if err != nil {
			return fmt.Errorf("updating dependency versions: %w", err)
		}

		if err = r.Create(ctx, desired); err != nil {
			logger.Error(err, "Failed creating new resource")
			return err
		}

		// updating `existing` with newly created resource
		err = r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
		if err != nil {
			logger.Error(err, "Failed to get newly created resource")
			return fmt.Errorf("getting newly created resource: %w", err)
		}
	}

	return nil
}

// EnsureUpdated ensures that existing resource `existing` is up-to-date with its 'desired' resource and its
// dependencies `deps`.
// In order to determine if the resource needs to be updated, the 'versions' annotations of `existing` and `updated`
// resources are compared.
// If dependency versions are equal, then we apply `patch` to `existing` resource with latest changes from `desired`.
func (r Reconciler) EnsureUpdated(
	ctx context.Context,
	owner,
	existing,
	desired client.Object,
	patch client.Patch,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx).WithValues(logfield.ResourceKV(desired)...)

	err := r.updateDependencyVersions(ctx, desired, deps...)
	if err != nil {
		logger.Error(err, "Failed to update dependency versions")
		return fmt.Errorf("updating dependency versions: %w", err)
	}

	existingDepVersions, err := getVersionsAnnotation(existing)
	if err != nil {
		logger.Error(err, "Failed to get existing resource versions annotation", logfield.ResourceKV(existing)...)
		return err
	}
	updatedDepVersions, err := getVersionsAnnotation(desired)
	if err != nil {
		logger.Error(err, "Failed to get updated resource versions annotation")
		return err
	}

	if !maps.Equal(updatedDepVersions, existingDepVersions) {
		logger.V(1).Info("Updating resource")

		if err = ctrl.SetControllerReference(owner, desired, r.Scheme); err != nil {
			logger.Error(err, "Failed to set controller reference")
			return fmt.Errorf("setting controller reference: %w", err)
		}

		if err = r.Update(ctx, desired); err != nil {
			logger.Error(err, "Failed to update resource")
			return fmt.Errorf("updating resource: %w", err)
		}

		return nil
	}

	logger = logger.WithValues(logfield.ResourceKV(existing)...)
	logger.V(1).Info("Patching resource")
	if err = r.Patch(ctx, existing, patch); err != nil {
		logger.Error(err, "Failed to patch resource")
		return fmt.Errorf("patching resource: %w", err)
	}

	return nil
}

func (r Reconciler) reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired client.Object,
	patcher patchFunc,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx).WithValues(logfield.ResourceKV(desired)...)

	reconcileImpl := func() error {
		var existing client.Object

		// resolve type of existing
		{
			switch desired.(type) {
			case *corev1.ConfigMap:
				existing = &corev1.ConfigMap{}
			case *corev1.Secret:
				existing = &corev1.Secret{}
			case *batchv1.CronJob:
				existing = &batchv1.CronJob{}
			case *batchv1.Job:
				existing = &batchv1.Job{}
			case *corev1.Service:
				existing = &corev1.Service{}
			case *appsv1.StatefulSet:
				existing = &appsv1.StatefulSet{}
			case *appsv1.DaemonSet:
				existing = &appsv1.DaemonSet{}
			case *corev1.ServiceAccount:
				existing = &corev1.ServiceAccount{}
			case *rbacv1.Role:
				existing = &rbacv1.Role{}
			case *rbacv1.RoleBinding:
				existing = &rbacv1.RoleBinding{}
			case *kruisev1b1.StatefulSet:
				existing = &kruisev1b1.StatefulSet{}
			case *otelv1beta1.OpenTelemetryCollector:
				existing = &otelv1beta1.OpenTelemetryCollector{}
			case *appsv1.Deployment:
				existing = &appsv1.Deployment{}
			case *prometheusv1.PodMonitor:
				existing = &prometheusv1.PodMonitor{}
			case *mariadbv1alpha1.MariaDB:
				existing = &mariadbv1alpha1.MariaDB{}
			case *mariadbv1alpha1.Grant:
				existing = &mariadbv1alpha1.Grant{}
			case *apparmor.AppArmorProfile:
				existing = &apparmor.AppArmorProfile{}
			case *slurmv1alpha1.JailedConfig:
				existing = &slurmv1alpha1.JailedConfig{}
			default:
				return errors.New(fmt.Sprintf("unimplemented resolver for resource type %T", desired))
			}
		}

		err := r.EnsureDeployed(ctx, cluster, existing, desired, deps...)
		if err != nil {
			logger.Error(err, "Failed to deploy")
			return fmt.Errorf("deploying: %w", err)
		}

		patch, err := patcher(existing, desired)
		if err != nil {
			logger.Error(err, "Failed to patch")
			return fmt.Errorf("patching: %w", err)
		}

		err = r.EnsureUpdated(ctx, cluster, existing, desired, patch, deps...)
		if err != nil {
			logger.Error(err, "Failed to update")
			return fmt.Errorf("updating: %w", err)
		}

		return nil
	}

	if err := reconcileImpl(); err != nil {
		logger.Error(err, "Failed to reconcile")
		return fmt.Errorf("reconciling: %w", err)
	}
	return nil
}
