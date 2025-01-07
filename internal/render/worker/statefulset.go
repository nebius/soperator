package worker

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
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
	secrets *slurmv1.Secrets,
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

	volumes, pvcTemplateSpecs, err := renderVolumesAndClaimTemplateSpecs(
		clusterName, secrets, volumeSources, worker,
	)
	if err != nil {
		return appsv1.StatefulSet{}, fmt.Errorf("rendering volumes and claim template specs: %w", err)
	}

	// Since 1.29 is native sidecar support, we can use the native restart policy
	initContainers := []corev1.Container{
		common.RenderContainerMunge(&worker.ContainerMunge),
	}
	if clusterType == consts.ClusterTypeGPU {
		initContainers = append(initContainers, renderContainerToolkitValidation(&worker.ContainerToolkitValidation))
	}

	slurmdContainer, err := renderContainerSlurmd(
		&worker.ContainerSlurmd,
		worker.JailSubMounts,
		clusterName,
		clusterType,
		worker.CgroupVersion,
		worker.EnableGDRCopy,
		worker.SlurmNodeExtra,
	)
	if err != nil {
		return appsv1.StatefulSet{}, fmt.Errorf("rendering slurmd container: %w", err)
	}

	replicas := &worker.StatefulSet.Replicas
	if check.IsMaintenanceActive(worker.Maintenance) {
		replicas = ptr.To(consts.ZeroReplicas)
	}

	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      worker.StatefulSet.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy: consts.PodManagementPolicy,
			ServiceName:         worker.Service.Name,
			Replicas:            replicas,
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
					Annotations: renderAnnotations(worker, clusterName, namespace),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: naming.BuildServiceAccountWorkerName(clusterName),
					Affinity:           nodeFilter.Affinity,
					NodeSelector:       nodeFilter.NodeSelector,
					Tolerations:        nodeFilter.Tolerations,
					InitContainers:     initContainers,
					Containers: []corev1.Container{
						slurmdContainer,
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

func renderAnnotations(worker *values.SlurmWorker, clusterName, namespace string) map[string]string {
	mungeAppArmorProfile := worker.ContainerMunge.AppArmorProfile
	workerAppArmorProfile := worker.ContainerSlurmd.AppArmorProfile

	if worker.UseDefaultAppArmorProfile {
		workerAppArmorProfile = fmt.Sprintf("%s/%s", "localhost", naming.BuildAppArmorProfileName(clusterName, namespace))
		mungeAppArmorProfile = fmt.Sprintf("%s/%s", "localhost", naming.BuildAppArmorProfileName(clusterName, namespace))
	}

	annotations := map[string]string{
		fmt.Sprintf(
			"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameSlurmd,
		): workerAppArmorProfile,
		fmt.Sprintf(
			"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameMunge,
		): mungeAppArmorProfile,
		consts.DefaultContainerAnnotationName: consts.ContainerNameSlurmd,
	}

	return annotations
}
