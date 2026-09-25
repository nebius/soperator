package nodesetcontroller

import (
	"context"
	"fmt"
	"reflect"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/values"
)

const powerPodOwnerIndex = "powerState.statefulSetOwner"

func powerPodOwnerKeys(obj client.Object) []string {
	owner := metav1.GetControllerOf(obj)
	if owner == nil || owner.Kind != "StatefulSet" || owner.APIVersion != kruisev1b1.GroupVersion.String() {
		return nil
	}
	return []string{owner.Name}
}

// reconcilePowerStateReady observes pods from the shared cache after resource reconciliation.
// Pending transitions use the existing reconciliation timer, avoiding a full reconcile per pod event.
func (r *NodeSetReconciler) reconcilePowerStateReady(ctx context.Context, nodeSet *slurmv1alpha1.NodeSet, nodeSetValues *values.SlurmNodeSet, powerState *slurmv1alpha1.NodeSetPowerState) (bool, error) {
	sts := &kruisev1b1.StatefulSet{}
	err := r.Get(ctx, client.ObjectKey{Namespace: nodeSet.Namespace, Name: nodeSetValues.StatefulSet.Name}, sts)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("get power state StatefulSet: %w", err)
	}
	ready := false
	if err == nil {
		var pods corev1.PodList
		if err := r.List(ctx, &pods, client.InNamespace(nodeSet.Namespace), client.MatchingFields{powerPodOwnerIndex: sts.Name}); err != nil {
			return false, fmt.Errorf("list cached power state pods: %w", err)
		}
		ready = (!check.IsMaintenanceActive(nodeSetValues.Maintenance) || len(powerState.Spec.ActiveNodes) == 0) &&
			sts.DeletionTimestamp == nil && sts.Status.ObservedGeneration >= sts.Generation &&
			sts.Spec.Replicas != nil && *sts.Spec.Replicas == int32(len(powerState.Spec.ActiveNodes)) &&
			powerPodsReady(sts, pods.Items, powerState.Spec.ActiveNodes)
	}
	condition := metav1.Condition{
		Type:               slurmv1alpha1.ConditionNodeSetPowerReady,
		ObservedGeneration: nodeSet.Generation,
		Status:             metav1.ConditionFalse,
		Reason:             "Reconciling",
		Message:            "Waiting for all active pods to be ready and inactive pods to be deleted",
	}
	if ready {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "PodsReady"
		condition.Message = "All active pods are ready and inactive pods are deleted"
	}
	applied := slurmv1alpha1.AppliedPowerState{
		UID:         powerState.UID,
		Generation:  powerState.Generation,
		ActiveNodes: make([]int32, len(powerState.Spec.ActiveNodes)),
	}
	copy(applied.ActiveNodes, powerState.Spec.ActiveNodes)
	err = r.patchStatus(ctx, nodeSet, func(status *slurmv1alpha1.NodeSetStatus) bool {
		changed := !reflect.DeepEqual(status.AppliedPowerState, &applied)
		status.AppliedPowerState = &applied
		return meta.SetStatusCondition(&status.Conditions, condition) || changed
	})
	return ready, err
}

func powerPodsReady(sts *kruisev1b1.StatefulSet, pods []corev1.Pod, active []int32) bool {
	wanted := make(map[string]bool, len(active))
	for _, ordinal := range active {
		wanted[fmt.Sprintf("%s-%d", sts.Name, ordinal)] = true
	}
	for i := range pods {
		pod := &pods[i]
		owner := metav1.GetControllerOf(pod)
		// Old pods from a recreated StatefulSet must finish terminating too.
		if owner == nil || owner.UID != sts.UID || !wanted[pod.Name] || pod.DeletionTimestamp != nil {
			return false
		}
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready {
			return false
		}
		delete(wanted, pod.Name)
	}
	return len(wanted) == 0
}

func (r *NodeSetReconciler) clearPowerStateReady(ctx context.Context, nodeSet *slurmv1alpha1.NodeSet) error {
	return r.patchStatus(ctx, nodeSet, func(status *slurmv1alpha1.NodeSetStatus) bool {
		changed := status.AppliedPowerState != nil || meta.FindStatusCondition(status.Conditions, slurmv1alpha1.ConditionNodeSetPowerReady) != nil
		status.AppliedPowerState = nil
		meta.RemoveStatusCondition(&status.Conditions, slurmv1alpha1.ConditionNodeSetPowerReady)
		return changed
	})
}
