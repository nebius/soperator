package worker

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
)

func RenderDaemonSet(
	namespace,
	clusterName,
	K8sNodeFilterName string,
	nodeFilters []slurmv1.K8sNodeFilter,
) appsv1.DaemonSet {
	labels := common.RenderLabels(consts.ComponentTypeWorker, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeWorker, clusterName)

	nodeFilter := utils.MustGetBy(
		nodeFilters,
		K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)

	initContainers := []corev1.Container{
		renderContainerNodeSysctl(),
	}

	return appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "k8s-node-sysctl",
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
				Spec: corev1.PodSpec{
					Affinity:     nodeFilter.Affinity,
					NodeSelector: nodeFilter.NodeSelector,
					Tolerations:  nodeFilter.Tolerations,
					DNSConfig: &corev1.PodDNSConfig{
						Searches: []string{
							naming.BuildServiceFQDN(consts.ComponentTypeWorker, namespace, clusterName),
						},
					},
					InitContainers: initContainers,
					Containers: []corev1.Container{
						renderContainerNodeSysctlSleep(),
					},
				},
			},
		},
	}
}
