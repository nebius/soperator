package controller

import (
	"fmt"

	appspub "github.com/openkruise/kruise-api/apps/pub"
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils/sliceutils"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderStatefulSet renders new [kruisev1b1.StatefulSet] containing Slurm controller pods
func RenderStatefulSet(
	namespace,
	clusterName string,
	nodeFilters []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
	controller *values.SlurmController,
	accountingEnabled bool,
) (kruisev1b1.StatefulSet, error) {
	labels := common.RenderLabels(consts.ComponentTypeController, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeController, clusterName)

	labels[consts.LabelControllerType] = consts.LabelControllerTypeMain
	matchLabels[consts.LabelControllerType] = consts.LabelControllerTypeMain

	nodeFilter := sliceutils.MustGetBy(
		nodeFilters,
		controller.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)

	volumes, pvcTemplateSpecs, err := renderVolumesAndClaimTemplateSpecs(
		clusterName, volumeSources, controller)
	if err != nil {
		return kruisev1b1.StatefulSet{}, fmt.Errorf("rendering volumes and claim template specs: %w", err)
	}

	// Controller always has 1 replica
	replicas := ptr.To(consts.SingleReplicas)
	if check.IsMaintenanceActive(controller.Maintenance) {
		replicas = ptr.To(consts.ZeroReplicas)
	}

	systemInitContainers := []corev1.Container{
		common.RenderContainerMunge(&controller.ContainerMunge),
	}
	if accountingEnabled {
		systemInitContainers = append(systemInitContainers, renderContainerAccountingWaiter(&controller.ContainerSlurmctld))
	}
	initContainers, err := common.OrderInitContainers(systemInitContainers, controller.CustomInitContainers)
	if err != nil {
		return kruisev1b1.StatefulSet{}, fmt.Errorf("ordering init containers: %w", err)
	}

	return kruisev1b1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.StatefulSet.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: kruisev1b1.StatefulSetSpec{
			PodManagementPolicy: consts.PodManagementPolicy,
			ServiceName:         controller.Service.Name,
			Replicas:            replicas,
			UpdateStrategy: kruisev1b1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &kruisev1b1.RollingUpdateStatefulSetStrategy{
					PodUpdatePolicy: kruisev1b1.InPlaceIfPossiblePodUpdateStrategyType,
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			VolumeClaimTemplates: common.RenderVolumeClaimTemplates(
				consts.ComponentTypeController,
				namespace,
				clusterName,
				pvcTemplateSpecs,
			),
			VolumeClaimUpdateStrategy: kruisev1b1.VolumeClaimUpdateStrategy{
				Type: kruisev1b1.OnPodRollingUpdateVolumeClaimUpdateStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						consts.AnnotationDefaultContainerName: consts.ContainerNameSlurmctld,
					},
				},
				Spec: corev1.PodSpec{
					ReadinessGates: []corev1.PodReadinessGate{
						{
							ConditionType: appspub.InPlaceUpdateReady,
						},
					},
					HostUsers:      controller.HostUsers,
					Affinity:       nodeFilter.Affinity,
					NodeSelector:   nodeFilter.NodeSelector,
					Tolerations:    nodeFilter.Tolerations,
					InitContainers: initContainers,
					Containers: []corev1.Container{
						renderContainerSlurmctld(&controller.ContainerSlurmctld, controller.CustomVolumeMounts),
					},
					Volumes:                       volumes,
					RestartPolicy:                 corev1.RestartPolicyAlways,
					TerminationGracePeriodSeconds: ptr.To(common.DefaultPodTerminationGracePeriodSeconds),
					SecurityContext:               &corev1.PodSecurityContext{},
					SchedulerName:                 corev1.DefaultSchedulerName,
					DNSPolicy:                     corev1.DNSClusterFirst,
					PriorityClassName:             controller.PriorityClass,
				},
			},
		},
	}, nil
}
