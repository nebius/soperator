package nodesetcontroller

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/state"
	"nebius.ai/slurm-operator/internal/utils/resourcegetter"
)

func (r *NodeSetReconciler) reconcile(ctx context.Context, nodeSet *slurmv1alpha1.NodeSet) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// region Synchronous reconciliation
	{
		kind := nodeSet.GetObjectKind()
		key := client.ObjectKeyFromObject(nodeSet)
		if state.ReconciliationState.Present(kind, key) {
			logger.V(1).Info("Reconciliation skipped, as object is already present in reconciliation state",
				"kind", kind.GroupVersionKind().String(),
				"key", key.String(),
			)
			return ctrl.Result{}, nil
		}

		state.ReconciliationState.Set(kind, key)
		logger.V(1).Info("Reconciliation state set for object",
			"kind", kind.GroupVersionKind().String(),
			"key", key.String(),
		)

		defer func() {
			state.ReconciliationState.Remove(kind, key)
			logger.V(1).Info("Reconciliation state removed for object",
				"kind", kind.GroupVersionKind().String(),
				"key", key.String(),
			)
		}()
	}
	// endregion Synchronous reconciliation

	logger.Info("Starting reconciliation")

	// region Get parental cluster
	var (
		cluster *slurmv1.SlurmCluster
		err     error
		//
		clusterName   string
		hasClusterRef bool
	)
	if clusterName, hasClusterRef = nodeSet.GetAnnotations()[consts.AnnotationParentalClusterRefName]; hasClusterRef {
		cluster, err = resourcegetter.GetCluster(ctx, r.Client,
			types.NamespacedName{
				Namespace: nodeSet.Namespace,
				Name:      clusterName,
			},
		)
	} else {
		cluster, err = resourcegetter.GetClusterInNamespace(ctx, r.Client, nodeSet.Namespace)
	}
	if err != nil || cluster == nil {
		logger.Error(err, "Failed to get parental cluster in namespace")
		return ctrl.Result{
			RequeueAfter: time.Minute,
		}, errors.Wrap(err, "getting parental cluster")
	}

	if !hasClusterRef {
		nodeSetBase := nodeSet.DeepCopy()
		maps.Insert(nodeSet.ObjectMeta.Annotations, func(yield func(string, string) bool) {
			if !yield(consts.AnnotationParentalClusterRefName, cluster.Name) {
				return
			}
		})
		if err = r.Patch(ctx, nodeSet, client.MergeFrom(nodeSetBase)); err != nil {
			logger.Error(err, "Failed to patch parental cluster annotation")
			return ctrl.Result{
				RequeueAfter: time.Minute,
			}, errors.Wrap(err, "patching parental cluster annotation")
		}
	}
	// endregion Get parental cluster

	logger.Info("Finished reconciliation")

	return ctrl.Result{}, nil
}

func (r *NodeSetReconciler) setUpConditions(ctx context.Context, nodeSet *slurmv1alpha1.NodeSet) error {
	patch := client.MergeFrom(nodeSet.DeepCopy())

	for _, conditionType := range []string{
		slurmv1alpha1.ConditionNodeSetConfigUpdated,
		slurmv1alpha1.ConditionNodeSetConfigDynamicUpdated,
		slurmv1alpha1.ConditionNodeSetStatefulSetUpdated,
		slurmv1alpha1.ConditionNodeSetPodsReady,
		slurmv1alpha1.ConditionNodeSetStatefulSetTerminated,
	} {
		if cond := meta.FindStatusCondition(nodeSet.Status.Conditions, conditionType); cond == nil {
			continue
		}
		nodeSet.Status.SetCondition(metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionUnknown,
			Reason:  "SetUpCondition",
			Message: "The object is not ready yet.",
		})
	}

	if err := r.Status().Patch(ctx, nodeSet, patch); err != nil {
		log.FromContext(ctx).Error(err, "Failed to patch status")
		return fmt.Errorf("patching %s status: %w", slurmv1alpha1.KindNodeSet, err)
	}

	return nil
}
