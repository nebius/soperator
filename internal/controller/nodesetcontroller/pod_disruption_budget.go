package nodesetcontroller

import (
	"context"
	"fmt"
	"maps"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/values"
)

func (r *NodeSetReconciler) reconcilePodDisruptionBudget(
	ctx context.Context, owner *slurmv1alpha1.NodeSet, nodeSet *values.SlurmNodeSet,
) error {
	desired := worker.RenderPodDisruptionBudget(nodeSet)
	existing := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{
		Name: desired.Name, Namespace: desired.Namespace,
	}}
	if nodeSet.UpdateStrategy != consts.UpdateStrategySlurmAwareRollingUpdate {
		if err := r.Get(ctx, client.ObjectKeyFromObject(existing), existing); err != nil {
			return client.IgnoreNotFound(err)
		}
		if !metav1.IsControlledBy(existing, owner) {
			return nil
		}
		return client.IgnoreNotFound(r.Delete(ctx, existing, client.Preconditions{UID: &existing.UID}))
	}
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, existing, func() error {
		if err := controllerutil.SetControllerReference(owner, existing, r.Scheme); err != nil {
			return err
		}
		if existing.Labels == nil {
			existing.Labels = make(map[string]string)
		}
		maps.Copy(existing.Labels, desired.Labels)
		existing.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return fmt.Errorf("reconcile worker pod disruption budget: %w", err)
	}
	return nil
}
