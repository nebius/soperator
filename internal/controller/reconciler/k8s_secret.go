package reconciler

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/logfield"
)

type SecretReconciler struct {
	*Reconciler
}

var (
	_ patcher = &SecretReconciler{}
)

func NewSecretReconciler(r *Reconciler) *SecretReconciler {
	return &SecretReconciler{
		Reconciler: r,
	}
}

func (r *SecretReconciler) Reconcile(
	ctx context.Context,
	owner client.Object,
	desired *corev1.Secret,
	deps ...metav1.Object,
) error {
	if err := r.reconcile(ctx, owner, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile Secret")
		return fmt.Errorf("reconciling Secret: %w", err)
	}
	return nil
}

func (r *SecretReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *corev1.Secret) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		dst.Data = src.Data

		return res
	}
	return patchImpl(existing.(*corev1.Secret), desired.(*corev1.Secret)), nil
}
