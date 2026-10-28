package worker

import (
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderPodDisruptionBudget blocks eviction until the worker handoff is complete.
func RenderPodDisruptionBudget(nodeSet *values.SlurmNodeSet) *policyv1.PodDisruptionBudget {
	labels := common.RenderLabels(consts.ComponentTypeNodeSet, nodeSet.ParentalCluster.Name)
	labels[consts.LabelNodeSetKey] = nodeSet.Name
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeNodeSet, nodeSet.ParentalCluster.Name)
	matchLabels[consts.LabelNodeSetKey] = nodeSet.Name
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeSet.StatefulSet.Name,
			Namespace: nodeSet.ParentalCluster.Namespace,
			Labels:    labels,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable:             ptr.To(intstr.FromInt32(0)),
			UnhealthyPodEvictionPolicy: ptr.To(policyv1.IfHealthyBudget),
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
				// NotIn also protects existing and newly created pods without this label.
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      consts.LabelSoperatorWorkerOperationPhase,
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{consts.LabelSoperatorWorkerOperationPhaseReady},
				}},
			},
		},
	}
}
