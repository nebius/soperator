package clustercontroller

import (
	"context"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

// mapObjectsToReconcileRequests is used to watch not-owned resource and created reconcile requests for SlurmCluster objects
func (r *SlurmClusterReconciler) mapObjectsToReconcileRequests(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	var (
		fieldPaths = []string{
			consts.IndexFieldSecretMungeKey,
		}
		res []reconcile.Request
	)

	for _, f := range fieldPaths {
		clusters := &slurmv1.SlurmClusterList{}
		opt := &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(f, obj.GetName()),
			Namespace:     obj.GetNamespace(),
		}
		err := r.List(ctx, clusters, opt)
		if err != nil {
			return []ctrl.Request{}
		}

		for _, item := range clusters.Items {
			res = append(res, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      item.GetName(),
					Namespace: item.GetNamespace(),
				},
			})
		}
	}

	return res
}

func indexFields(mgr ctrl.Manager) error {
	if err := indexField(mgr, consts.IndexFieldSecretMungeKey, indexFuncSecretMungeKey); err != nil {
		return err
	}
	return nil
}

func indexField(mgr ctrl.Manager, field string, fn client.IndexerFunc) error {
	return mgr.GetFieldIndexer().IndexField(context.Background(), &slurmv1.SlurmCluster{}, field, fn)
}

func indexFuncSecretMungeKey(obj client.Object) []string {
	cluster := obj.(*slurmv1.SlurmCluster)
	if cluster.Spec.Secrets.MungeKey.Name != "" {
		return []string{cluster.Spec.Secrets.MungeKey.Name}
	}
	return []string{}
}
