package reconciler

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

type NodeSetPowerStateReconciler struct {
	*Reconciler
}

var (
	_ patcher = &NodeSetPowerStateReconciler{}
)

func NewNodeSetPowerStateReconciler(r *Reconciler) *NodeSetPowerStateReconciler {
	return &NodeSetPowerStateReconciler{
		Reconciler: r,
	}
}

// Reconcile ensures the NodeSetPowerState CR exists and is up to date
func (r *NodeSetPowerStateReconciler) Reconcile(
	ctx context.Context,
	owner client.Object,
	desired *slurmv1alpha1.NodeSetPowerState,
	deps ...metav1.Object,
) error {
	existing := &slurmv1alpha1.NodeSetPowerState{}
	if err := r.EnsureDeployed(ctx, owner, existing, desired, deps...); err != nil {
		return fmt.Errorf("ensure power state: %w", err)
	}
	if existing.Spec.NodeSetRef == desired.Spec.NodeSetRef {
		return nil
	}
	patch, err := r.patch(existing, desired)
	if err != nil {
		return err
	}
	if err := r.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch power state: %w", err)
	}

	return nil
}

func (r *NodeSetPowerStateReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *slurmv1alpha1.NodeSetPowerState) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		// We don't overwrite activeNodes here because it's managed by the power-manager binary.
		// The NodeSet controller only creates/ensures the NodeSetPowerState exists.
		// The spec.nodeSetRef should always match.
		dst.Spec.NodeSetRef = src.Spec.NodeSetRef

		return res
	}
	return patchImpl(existing.(*slurmv1alpha1.NodeSetPowerState), desired.(*slurmv1alpha1.NodeSetPowerState)), nil
}
