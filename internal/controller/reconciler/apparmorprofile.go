package reconciler

import (
	"context"
	"errors"
	"fmt"

	apparmor "sigs.k8s.io/security-profiles-operator/api/apparmorprofile/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/logfield"
)

type AppArmorProfileReconciler struct {
	*Reconciler
}

var (
	_ patcher = &AppArmorProfileReconciler{}
)

func NewAppArmorProfileReconciler(r *Reconciler) *AppArmorProfileReconciler {
	return &AppArmorProfileReconciler{
		Reconciler: r,
	}
}

func (r *AppArmorProfileReconciler) Reconcile(
	ctx context.Context,
	owner client.Object,
	desired *apparmor.AppArmorProfile,
	deps ...metav1.Object,
) error {
	if desired == nil {
		return errors.New("AppArmorProfile is not needed")
	}
	if err := r.reconcile(ctx, owner, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile AppArmorProfile ")
		return fmt.Errorf("reconciling AppArmorProfile: %w", err)
	}
	return nil
}

func (r *AppArmorProfileReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *apparmor.AppArmorProfile) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec.Policy = src.Spec.Policy
		return res
	}
	return patchImpl(existing.(*apparmor.AppArmorProfile), desired.(*apparmor.AppArmorProfile)), nil
}
