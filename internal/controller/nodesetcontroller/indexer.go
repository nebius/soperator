package nodesetcontroller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

const (
	configMapSupervisordField = ".spec.configMapRefSupervisord"
	configMapSshdField        = ".spec.configMapRefSshd"
)

func (r *NodeSetReconciler) setupConfigMapIndexer(mgr ctrl.Manager) error {
	indexers := map[string]func(*slurmv1alpha1.NodeSet) string{
		configMapSupervisordField: func(ns *slurmv1alpha1.NodeSet) string {
			return ns.Spec.ConfigMapRefSupervisord
		},
		configMapSshdField: func(ns *slurmv1alpha1.NodeSet) string {
			return ns.Spec.ConfigMapRefSSHD
		},
	}

	for field, extractFunc := range indexers {
		err := mgr.GetFieldIndexer().IndexField(
			context.Background(),
			&slurmv1alpha1.NodeSet{},
			field,
			func(rawObj client.Object) []string {
				nodeSet := rawObj.(*slurmv1alpha1.NodeSet)
				value := extractFunc(nodeSet)
				if value == "" {
					return nil
				}
				return []string{value}
			},
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *NodeSetReconciler) findObjectsForConfigMap(
	ctx context.Context,
	configmap client.Object,
) []reconcile.Request {
	configMap, ok := configmap.(*corev1.ConfigMap)
	if !ok {
		return nil
	}

	matchingFields := []string{
		configMapSupervisordField,
		configMapSshdField,
	}

	attachedNodeSets := &slurmv1alpha1.NodeSetList{}

	var requests []reconcile.Request

	for _, field := range matchingFields {
		listOpts := []client.ListOption{
			client.MatchingFields{field: configMap.Name},
			client.InNamespace(configMap.Namespace),
		}
		if err := r.Client.List(ctx, attachedNodeSets, listOpts...); err != nil {
			continue
		}
		for _, nodeSet := range attachedNodeSets.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: nodeSet.Namespace,
					Name:      nodeSet.Name,
				},
			})
		}
	}

	return requests
}
