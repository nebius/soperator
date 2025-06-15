package reconciler

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
)

type ServiceReconciler struct {
	*Reconciler
}

var (
	_ patcher = &ServiceReconciler{}
)

func NewServiceReconciler(r *Reconciler) *ServiceReconciler {
	return &ServiceReconciler{
		Reconciler: r,
	}
}

func (r *ServiceReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *corev1.Service,
	name *string,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx)
	if desired == nil {
		// If desired is nil, delete the Service
		if name == nil {
			logger.V(1).Info("Service is not needed, skipping deletion")
			return nil
		}
		logger.V(1).Info(fmt.Sprintf("Deleting Service %s, because Service is not needed", *name))
		return r.deleteIfOwnedByController(ctx, cluster, cluster.Namespace, *name)
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		logger.V(1).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile Service")
		return fmt.Errorf("reconciling Service: %w", err)
	}
	return nil
}

func (r *ServiceReconciler) deleteIfOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	namespace,
	name string,
) error {
	logger := log.FromContext(ctx)
	service, err := r.getService(ctx, namespace, name)
	if apierrors.IsNotFound(err) {
		logger.V(1).Info("Service not found, skipping deletion")
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting Service: %w", err)
	}

	if !metav1.IsControlledBy(service, cluster) {
		logger.V(1).Info("Service is not owned by controller, skipping deletion")
		return nil
	}

	if err := r.Delete(ctx, service); err != nil {
		return fmt.Errorf("deleting Service: %w", err)
	}
	return nil
}

func (r *ServiceReconciler) getService(ctx context.Context, namespace, name string) (*corev1.Service, error) {
	service := &corev1.Service{}
	err := r.Get(
		ctx,
		client.ObjectKey{
			Namespace: namespace,
			Name:      name,
		},
		service,
	)
	return service, err
}

func (r *ServiceReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *corev1.Service) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		if len(src.Annotations) > 0 {
			if dst.Annotations == nil {
				dst.Annotations = make(map[string]string, len(src.Annotations))
			}
			maps.Copy(dst.Annotations, src.Annotations) // dst ← src
		}

		dst.Spec.Type = src.Spec.Type
		dst.Spec.Ports = append([]corev1.ServicePort{}, src.Spec.Ports...)

		return res
	}
	return patchImpl(existing.(*corev1.Service), desired.(*corev1.Service)), nil
}
