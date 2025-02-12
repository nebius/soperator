package nodeconfigurator

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// RenderDaemonSet renders the DaemonSet for the node-configurator
func RenderDaemonSet(
	nodeConfigurator *slurmv1alpha1.NodeConfigurator,
	namespace string,
) *appsv1.DaemonSet {
	if nodeConfigurator == nil {
		return nil
	}

	labels, matchLabels := renderLabels(nodeConfigurator.Name)

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildDaemonSetName(nodeConfigurator.Name, consts.NodeConfiguratorName),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: renderPodSpec(nodeConfigurator.Spec),
			},
		},
	}
}
