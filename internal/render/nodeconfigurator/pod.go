package nodeconfigurator

import (
	"slices"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
)

func renderPodSpec(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) corev1.PodSpec {
	// It allows to run a command from systemd's namespace for example (pid 1)
	// This relies on hostPID:true and privileged:true to enter host mount space
	// rebooter container will use this for rebooting the node
	hostPID := nodeConfigurator.Rebooter.Enabled

	containers := renderContainers(nodeConfigurator)

	affinity := getAffinity(nodeConfigurator)
	tolerations := getTolerations(nodeConfigurator)
	nodeSelector := getNodeSelector(nodeConfigurator)
	serviceAccountName := getServiceAccountName(nodeConfigurator)
	priorityClassName := getPriorityClassName(nodeConfigurator)
	hostUsers := getHostUsers(nodeConfigurator)

	initContainers := slices.Clone(nodeConfigurator.InitContainers)

	// Lexicographic sorting init containers by their names to have implicit ordering functionality
	sort.Slice(initContainers, func(i, j int) bool {
		return initContainers[i].Name < initContainers[j].Name
	})

	return corev1.PodSpec{
		HostNetwork:           nodeConfigurator.HostNetwork,
		HostIPC:               nodeConfigurator.HostIPC,
		ShareProcessNamespace: ptr.To(nodeConfigurator.ShareProcessNamespace),
		HostUsers:             hostUsers,
		HostPID:               hostPID,
		Affinity:              affinity,
		NodeSelector:          nodeSelector,
		Tolerations:           tolerations,
		InitContainers:        initContainers,
		Containers:            containers,
		ServiceAccountName:    serviceAccountName,
		PriorityClassName:     priorityClassName,
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

func getAffinity(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) *corev1.Affinity {
	if nodeConfigurator.Rebooter.Enabled {
		return nodeConfigurator.Rebooter.Affinity
	}
	return nodeConfigurator.CustomContainer.Affinity
}

func getTolerations(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) []corev1.Toleration {
	if nodeConfigurator.Rebooter.Enabled {
		return nodeConfigurator.Rebooter.Tolerations
	}
	return nodeConfigurator.CustomContainer.Tolerations
}

func getNodeSelector(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) map[string]string {
	if nodeConfigurator.Rebooter.Enabled {
		return nodeConfigurator.Rebooter.NodeSelector
	}
	return nodeConfigurator.CustomContainer.NodeSelector
}

func getServiceAccountName(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) string {
	if nodeConfigurator.Rebooter.Enabled {
		return nodeConfigurator.Rebooter.ServiceAccountName
	}
	return nodeConfigurator.CustomContainer.ServiceAccountName
}

func getPriorityClassName(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) string {
	if nodeConfigurator.Rebooter.Enabled {
		return nodeConfigurator.Rebooter.PriorityClassName
	}
	return nodeConfigurator.CustomContainer.PriorityClassName
}

func getHostUsers(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) *bool {
	if nodeConfigurator.Rebooter.Enabled {
		return nodeConfigurator.Rebooter.HostUsers
	}
	return nodeConfigurator.CustomContainer.HostUsers
}
