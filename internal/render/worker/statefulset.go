package worker

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderStatefulSet renders new [appsv1.StatefulSet] containing Slurm worker pods
func RenderStatefulSet(
	namespace,
	clusterName string,
	clusterType consts.ClusterType,
	nodeFilters []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
	worker *values.SlurmWorker,
) (appsv1.StatefulSet, error) {
	labels := common.RenderLabels(consts.ComponentTypeWorker, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeWorker, clusterName)

	nodeFilter := utils.MustGetBy(
		nodeFilters,
		worker.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)

	volumes, pvcTemplateSpecs, err := renderVolumesAndClaimTemplateSpecs(clusterName, volumeSources, worker)
	if err != nil {
		return appsv1.StatefulSet{}, fmt.Errorf("rendering volumes and claim template specs: %w", err)
	}

	annotations := map[string]string{
		fmt.Sprintf(
			"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameSlurmd,
		): worker.ContainerSlurmd.AppArmorProfile,
		fmt.Sprintf(
			"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameMunge,
		): worker.ContainerMunge.AppArmorProfile,
		consts.DefaultContainerAnnotationName: consts.ContainerNameSlurmd,
	}

	var initContainers []corev1.Container
	if clusterType == consts.ClusterTypeGPU {
		initContainers = append(initContainers, renderContainerToolkitValidation(&worker.ContainerToolkitValidation))
	}

	slurmdContainer := renderContainerSlurmd(
		&worker.ContainerSlurmd,
		worker.JailSubMounts,
		clusterName,
		clusterType,
		worker.CgroupVersion,
	)

	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      worker.StatefulSet.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy: consts.PodManagementPolicy,
			ServiceName:         worker.Service.Name,
			Replicas:            &worker.StatefulSet.Replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable: &worker.StatefulSet.MaxUnavailable,
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			VolumeClaimTemplates: common.RenderVolumeClaimTemplates(
				consts.ComponentTypeWorker,
				namespace,
				clusterName,
				pvcTemplateSpecs,
			),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: naming.BuildServiceAccountWorkerName(clusterName),
					Affinity:           nodeFilter.Affinity,
					NodeSelector:       nodeFilter.NodeSelector,
					Tolerations:        nodeFilter.Tolerations,
					InitContainers:     initContainers,
					Containers: []corev1.Container{
						slurmdContainer,
						common.RenderContainerMunge(&worker.ContainerMunge),
					},
					Volumes: volumes,
					DNSConfig: &corev1.PodDNSConfig{
						Searches: []string{
							naming.BuildServiceFQDN(consts.ComponentTypeWorker, namespace, clusterName),
						},
					},
				},
			},
		},
	}, nil
}
