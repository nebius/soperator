package reconciler

import (
	"context"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	apparmorprofileapi "sigs.k8s.io/security-profiles-operator/api/apparmorprofile/v1alpha1"
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
	cluster *slurmv1.SlurmCluster,
	desired *apparmorprofileapi.AppArmorProfile,
	deps ...metav1.Object,
) error {
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile AppArmor")
		return errors.Wrap(err, "reconciling AppArmor")
	}
	return nil
}

func (r *AppArmorProfileReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *apparmorprofileapi.AppArmorProfile) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		dst.Spec.Policy = src.Spec.Policy

		return res
	}
	return patchImpl(existing.(*apparmorprofileapi.AppArmorProfile), desired.(*apparmorprofileapi.AppArmorProfile)), nil
}
