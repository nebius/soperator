package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderStatefulSet renders new [appsv1.StatefulSet] containing Slurm controller pods
func RenderStatefulSet(
	namespace,
	clusterName string,
	nodeFilters []slurmv1.K8sNodeFilter,
	secrets *slurmv1.Secrets,
	volumeSources []slurmv1.VolumeSource,
	controller *values.SlurmController,
	slurmTopologyConfigMapRefName string,
) (appsv1.StatefulSet, error) {
	labels := common.RenderLabels(consts.ComponentTypeController, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeController, clusterName)

	nodeFilter := utils.MustGetBy(
		nodeFilters,
		controller.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)

	volumes, pvcTemplateSpecs, err := renderVolumesAndClaimTemplateSpecs(
		clusterName, volumeSources, controller, slurmTopologyConfigMapRefName)
	if err != nil {
		return appsv1.StatefulSet{}, fmt.Errorf("rendering volumes and claim template specs: %w", err)
	}

	replicas := &controller.StatefulSet.Replicas
	if check.IsMaintenanceActive(controller.Maintenance) {
		replicas = ptr.To(consts.ZeroReplicas)
	}

	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.StatefulSet.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy: consts.PodManagementPolicy,
			ServiceName:         controller.Service.Name,
			Replicas:            replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable: &controller.StatefulSet.MaxUnavailable,
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
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						fmt.Sprintf(
							"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameSlurmctld,
						): controller.ContainerSlurmctld.AppArmorProfile,
						fmt.Sprintf(
							"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameMunge,
						): controller.ContainerMunge.AppArmorProfile,
						consts.DefaultContainerAnnotationName: consts.ContainerNameSlurmctld,
					},
				},
				Spec: corev1.PodSpec{
					Affinity:     nodeFilter.Affinity,
					NodeSelector: nodeFilter.NodeSelector,
					Tolerations:  nodeFilter.Tolerations,
					InitContainers: append(
						controller.CustomInitContainers,
						common.RenderContainerMunge(&controller.ContainerMunge),
					),
					Containers: []corev1.Container{
						renderContainerSlurmctld(&controller.ContainerSlurmctld, controller.CustomVolumeMounts),
					},
					Volumes: volumes,
				},
			},
		},
	}, nil
}
