package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderStatefulSet renders new [appsv1.StatefulSet] containing Slurm controller pods
func RenderStatefulSet(cluster *values.SlurmCluster) (appsv1.StatefulSet, error) {
	labels := common.RenderLabels(consts.ComponentTypeController, cluster.Name)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeController, cluster.Name)

	stsVersion, podVersion, err := common.GenerateVersionsAnnotationPlaceholders()
	if err != nil {
		return appsv1.StatefulSet{}, fmt.Errorf("generating versions annotation placeholders: %w", err)
	}

	nodeFilter := utils.MustGetBy(
		cluster.NodeFilters,
		cluster.NodeController.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)

	volumes := []corev1.Volume{
		common.RenderVolumeSlurmConfigs(cluster),
		common.RenderVolumeMungeKey(cluster),
		common.RenderVolumeMungeSocket(),
	}
	if cluster.NodeController.VolumeSpool.VolumeSourceName != nil {
		volumes = append(volumes, common.RenderVolumeSpool(consts.ComponentTypeController, cluster))
	}
	if cluster.NodeController.VolumeJail.VolumeSourceName != nil {
		volumes = append(volumes, common.RenderVolumeJail(cluster))
	}

	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.NodeController.StatefulSet.Name,
			Namespace: cluster.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				consts.AnnotationVersions: string(stsVersion),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: cluster.NodeController.ContainerSlurmctld.Name,
			Replicas:    &cluster.NodeController.StatefulSet.Replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable: &cluster.NodeController.StatefulSet.MaxUnavailable,
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						consts.AnnotationVersions: string(podVersion),
						fmt.Sprintf(
							"%s/%s", consts.AnnotationApparmorKey, consts.ContainerSlurmctldName,
						): consts.AnnotationApparmorValueUnconfined,
						fmt.Sprintf(
							"%s/%s", consts.AnnotationApparmorKey, consts.ContainerMungeName,
						): consts.AnnotationApparmorValueUnconfined,
					},
				},
				Spec: corev1.PodSpec{
					Affinity:     nodeFilter.Affinity,
					NodeSelector: nodeFilter.NodeSelector,
					Tolerations:  nodeFilter.Tolerations,
					Containers: []corev1.Container{
						renderContainerSlurmctld(cluster),
						renderContainerMunge(cluster),
					},
					Volumes: volumes,
				},
			},
			VolumeClaimTemplates: common.RenderVolumeClaimTemplates(
				consts.ComponentTypeController,
				cluster,
				[]values.PVCTemplateSpec{{
					Name: common.RenderVolumeNameSpool(consts.ComponentTypeController),
					Spec: cluster.NodeController.VolumeSpool.VolumeClaimTemplateSpec,
				}, {
					Name: consts.VolumeNameJail,
					Spec: cluster.NodeController.VolumeJail.VolumeClaimTemplateSpec,
				}},
			),
		},
	}, nil
}
