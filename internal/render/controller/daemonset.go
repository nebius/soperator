package controller

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils/sliceutils"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderPlaceholderDaemonSet renders new [appsv1.DaemonSet] containing additional Slurm controller pods
func RenderPlaceholderDaemonSet(
	namespace,
	clusterName string,
	nodeFilters []slurmv1.K8sNodeFilter,
	controller *values.SlurmController,
	accountingEnabled bool,
) appsv1.DaemonSet {
	labels := common.RenderLabels(consts.ComponentTypeController, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeController, clusterName)

	labels[consts.LabelControllerType] = consts.LabelControllerTypePlaceholder
	matchLabels[consts.LabelControllerType] = consts.LabelControllerTypePlaceholder

	nodeFilter := sliceutils.MustGetBy(
		nodeFilters,
		controller.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)

	containers := []corev1.Container{
		renderContainerSlurmctldSleep(&controller.ContainerSlurmctld),
	}
	if accountingEnabled {
		containers = append(containers, renderContainerAccountingWaiterSleep(&controller.ContainerSlurmctld))
	}
	containers = append(containers, renderCustomContainersSleep(controller.CustomInitContainers)...)

	initContainers := []corev1.Container{
		common.RenderPlaceholderContainerMunge(&controller.ContainerMunge),
	}
	// Lexicographic sorting init containers by their names to have implicit ordering functionality
	sort.Slice(initContainers, func(i, j int) bool {
		return initContainers[i].Name < initContainers[j].Name
	})

	return appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.DaemonSet.Name + "-placeholder",
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxUnavailable: ptr.To(intstr.FromInt32(1)),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					HostUsers:                     controller.HostUsers,
					Affinity:                      nodeFilter.Affinity,
					NodeSelector:                  nodeFilter.NodeSelector,
					Tolerations:                   nodeFilter.Tolerations,
					InitContainers:                initContainers,
					Containers:                    containers,
					RestartPolicy:                 corev1.RestartPolicyAlways,
					TerminationGracePeriodSeconds: ptr.To(common.DefaultPodTerminationGracePeriodSeconds),
					SecurityContext:               &corev1.PodSecurityContext{},
					SchedulerName:                 corev1.DefaultSchedulerName,
					DNSPolicy:                     corev1.DNSClusterFirst,
					PriorityClassName:             controller.PriorityClass,
				},
			},
		},
	}
}

// renderContainerAccountingWaiterSleep renders accounting waiting init [corev1.Container] in sleep mode for DaemonSet
func renderContainerAccountingWaiterSleep(container *values.Container) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameWaitForAccounting,
		Image:           container.Image,
		ImagePullPolicy: container.ImagePullPolicy,
		Command:         []string{"sleep"},
		Args:            []string{"infinity"},
	}
}

// renderCustomContainersSleep converts custom init containers to sleep containers
func renderCustomContainersSleep(customInitContainers []corev1.Container) []corev1.Container {
	if len(customInitContainers) == 0 {
		return nil
	}

	result := make([]corev1.Container, len(customInitContainers))
	for i, container := range customInitContainers {
		sleepContainer := container.DeepCopy()
		sleepContainer.Command = []string{"sleep"}
		sleepContainer.Args = []string{"infinity"}
		sleepContainer.Resources = corev1.ResourceRequirements{}
		sleepContainer.VolumeMounts = nil
		sleepContainer.Env = nil
		result[i] = *sleepContainer
	}
	return result
}
