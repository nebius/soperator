package nodeconfigurator

import (
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

	initContainers := nodeConfigurator.InitContainers
	if len(nodeConfigurator.InitContainers) == 0 {
		initContainers = renderInitContainers()
	}

	containers := renderContainers(nodeConfigurator)

	affinity := getAffinity(nodeConfigurator)
	tolerations := getTolerations(nodeConfigurator)
	nodeSelector := getNodeSelector(nodeConfigurator)
	serviceAccountName := getServiceAccountName(nodeConfigurator)
	priorityClassName := getPriorityClassName(nodeConfigurator)

	return corev1.PodSpec{
		HostUsers:          ptr.To(false),
		HostPID:            hostPID,
		Affinity:           affinity,
		NodeSelector:       nodeSelector,
		Tolerations:        tolerations,
		InitContainers:     initContainers,
		Containers:         containers,
		ServiceAccountName: serviceAccountName,
		PriorityClassName:  priorityClassName,
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
	return nodeConfigurator.SleepContainer.Affinity
}

func getTolerations(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) []corev1.Toleration {
	if nodeConfigurator.Rebooter.Enabled {
		return nodeConfigurator.Rebooter.Tolerations
	}
	return nodeConfigurator.SleepContainer.Tolerations
}

func getNodeSelector(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) map[string]string {
	if nodeConfigurator.Rebooter.Enabled {
		return nodeConfigurator.Rebooter.NodeSelector
	}
	return nodeConfigurator.SleepContainer.NodeSelector
}

func getServiceAccountName(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) string {
	if nodeConfigurator.Rebooter.Enabled {
		return nodeConfigurator.Rebooter.ServiceAccountName
	}
	return nodeConfigurator.SleepContainer.ServiceAccountName
}

func getPriorityClassName(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) string {
	if nodeConfigurator.Rebooter.Enabled {
		return nodeConfigurator.Rebooter.PriorityClassName
	}
	return nodeConfigurator.SleepContainer.PriorityClassName
}
