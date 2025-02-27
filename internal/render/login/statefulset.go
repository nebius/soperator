package login

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

// RenderStatefulSet renders new [appsv1.StatefulSet] containing Slurm login pods
func RenderStatefulSet(
	namespace,
	clusterName string,
	clusterType consts.ClusterType,
	nodeFilters []slurmv1.K8sNodeFilter,
	secrets *slurmv1.Secrets,
	volumeSources []slurmv1.VolumeSource,
	login *values.SlurmLogin,
) (appsv1.StatefulSet, error) {
	labels := common.RenderLabels(consts.ComponentTypeLogin, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeLogin, clusterName)

	nodeFilter := utils.MustGetBy(
		nodeFilters,
		login.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)

	volumes, pvcTemplateSpecs, err := renderVolumesAndClaimTemplateSpecs(clusterName, secrets, volumeSources, login)
	if err != nil {
		return appsv1.StatefulSet{}, fmt.Errorf("rendering volumes and claim template specs: %w", err)
	}

	replicas := &login.StatefulSet.Replicas
	if check.IsMaintenanceActive(login.Maintenance) {
		replicas = ptr.To(consts.ZeroReplicas)
	}

	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      login.StatefulSet.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy: consts.PodManagementPolicy,
			ServiceName:         login.Service.Name,
			Replicas:            replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable: &login.StatefulSet.MaxUnavailable,
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			VolumeClaimTemplates: common.RenderVolumeClaimTemplates(
				consts.ComponentTypeLogin,
				namespace,
				clusterName,
				pvcTemplateSpecs,
			),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: renderAnnotations(login, clusterName, namespace),
				},
				Spec: corev1.PodSpec{
					Affinity:     nodeFilter.Affinity,
					NodeSelector: nodeFilter.NodeSelector,
					Tolerations:  nodeFilter.Tolerations,
					InitContainers: []corev1.Container{
						common.RenderContainerMunge(&login.ContainerMunge),
					},
					Containers: []corev1.Container{
						renderContainerSshd(clusterType, &login.ContainerSshd, login.JailSubMounts, login.CustomVolumeMounts),
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

func renderAnnotations(login *values.SlurmLogin, clusterName, namespace string) map[string]string {
	mungeAppArmorProfile := login.ContainerMunge.AppArmorProfile
	sshAppArmorProfile := login.ContainerSshd.AppArmorProfile

	if login.UseDefaultAppArmorProfile {
		sshAppArmorProfile = fmt.Sprintf("%s/%s", "localhost", naming.BuildAppArmorProfileName(clusterName, namespace))
	}

	annotations := map[string]string{
		fmt.Sprintf(
			"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameSshd,
		): sshAppArmorProfile,
		fmt.Sprintf(
			"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameMunge,
		): mungeAppArmorProfile,
		consts.DefaultContainerAnnotationName: consts.ContainerNameSshd,
	}

	return annotations
}
