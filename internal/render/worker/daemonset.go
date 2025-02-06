package worker

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
)

// TODO: Move this conmponent to the cpecifc CRD NodeConfigurator

// RenderDaemonSet renders the DaemonSet for the node-configurator
func RenderDaemonSet(
	rebooter slurmv1.Rebooter,
	clusterName,
	K8sNodeFilterName string,
	nodeFilters []slurmv1.K8sNodeFilter,
	maintenance *consts.MaintenanceMode,
) appsv1.DaemonSet {
	labels, matchLabels := renderLabels(clusterName)
	nodeFilter := getNodeFilter(K8sNodeFilterName, nodeFilters)
	initContainers := renderInitContainers()
	containers := renderContainers(rebooter)
	nodeFilter = setMaintenanceNodeSelector(nodeFilter, maintenance)
	nodeFilter = setPodAntiAffinity(nodeFilter)
	rebooter.ServiceAccountName = setDefaultServiceAccountName(rebooter.ServiceAccountName)

	return appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildDaemonSetName(consts.ComponentTypeNodeConfigurator),
			Namespace: rebooter.Namespace,
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
					// HostPID rights needed for rebooting the node
					HostPID:            true,
					Affinity:           nodeFilter.Affinity,
					NodeSelector:       nodeFilter.NodeSelector,
					Tolerations:        nodeFilter.Tolerations,
					InitContainers:     initContainers,
					Containers:         containers,
					ServiceAccountName: rebooter.ServiceAccountName,
				},
			},
		},
	}
}

// renderLabels renders the labels and matchLabels for the DaemonSet
func renderLabels(clusterName string) (map[string]string, map[string]string) {
	labels := common.RenderLabels(consts.ComponentTypeNodeConfigurator, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeNodeConfigurator, clusterName)
	labels[consts.LabelNodeConfiguratorKey] = consts.LabelNodeConfiguratorValue
	matchLabels[consts.LabelNodeConfiguratorKey] = consts.LabelNodeConfiguratorValue
	return labels, matchLabels
}

// getNodeFilter returns the K8sNodeFilter with the given name
func getNodeFilter(K8sNodeFilterName string, nodeFilters []slurmv1.K8sNodeFilter) slurmv1.K8sNodeFilter {
	return utils.MustGetBy(
		nodeFilters,
		K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)
}

// renderInitContainers renders the initContainers for the DaemonSet
func renderInitContainers() []corev1.Container {
	return []corev1.Container{
		renderContainerNodeSysctl(),
	}
}

// renderContainers renders the containers for the DaemonSet
func renderContainers(rebooter slurmv1.Rebooter) []corev1.Container {
	if rebooter.Enabled {
		return []corev1.Container{renderContainerRebooter(rebooter)}
	}
	return []corev1.Container{renderContainerNodeSysctlSleep()}
}

// rsetMaintenanceNodeSelector sets the maintenance node selector if the maintenance mode is active
func setMaintenanceNodeSelector(nodeFilter slurmv1.K8sNodeFilter, maintenance *consts.MaintenanceMode) slurmv1.K8sNodeFilter {
	if check.IsMaintenanceActive(maintenance) {
		nodeFilter.NodeSelector = map[string]string{
			"maintenance": "true",
		}
	}
	return nodeFilter
}

// setPodAntiAffinity sets the pod anti-affinity for the DaemonSet
func setPodAntiAffinity(nodeFilter slurmv1.K8sNodeFilter) slurmv1.K8sNodeFilter {
	nodeFilter.Affinity.PodAntiAffinity = &corev1.PodAntiAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
			{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						consts.LabelNodeConfiguratorKey: consts.LabelNodeConfiguratorValue,
					},
				},
				TopologyKey: "kubernetes.io/hostname",
			},
		},
	}
	return nodeFilter
}

// setDefaultServiceAccountName sets the default service account name if it is not provided
func setDefaultServiceAccountName(serviceAccountName string) string {
	if serviceAccountName == "" {
		return "default"
	}
	return serviceAccountName
}
