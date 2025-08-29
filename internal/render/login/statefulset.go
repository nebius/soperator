package login

import (
	"fmt"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
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

// RenderStatefulSet renders new [kruisev1b1.StatefulSet] containing Slurm login pods
func RenderStatefulSet(
	namespace,
	clusterName string,
	clusterType consts.ClusterType,
	nodeFilters []slurmv1.K8sNodeFilter,
	secrets *slurmv1.Secrets,
	volumeSources []slurmv1.VolumeSource,
	login *values.SlurmLogin,
) (kruisev1b1.StatefulSet, error) {
	labels := common.RenderLabels(consts.ComponentTypeLogin, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeLogin, clusterName)

	nodeFilter := utils.MustGetBy(
		nodeFilters,
		login.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)

	volumes, pvcTemplateSpecs, err := renderVolumesAndClaimTemplateSpecs(
		clusterName, secrets, volumeSources, login)
	if err != nil {
		return kruisev1b1.StatefulSet{}, fmt.Errorf("rendering volumes and claim template specs: %w", err)
	}

	replicas := &login.StatefulSet.Replicas
	if check.IsMaintenanceActive(login.Maintenance) {
		replicas = ptr.To(consts.ZeroReplicas)
	}

	return kruisev1b1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      login.StatefulSet.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: kruisev1b1.StatefulSetSpec{
			PodManagementPolicy: consts.PodManagementPolicy,
			ServiceName:         login.HeadlessService.Name,
			Replicas:            replicas,
			UpdateStrategy: kruisev1b1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &kruisev1b1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable:  &login.StatefulSet.MaxUnavailable,
					PodUpdatePolicy: kruisev1b1.RecreatePodUpdateStrategyType,
					Partition:       ptr.To(int32(0)),
					MinReadySeconds: ptr.To(int32(0)),
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
			VolumeClaimUpdateStrategy: kruisev1b1.VolumeClaimUpdateStrategy{
				Type: kruisev1b1.OnPodRollingUpdateVolumeClaimUpdateStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: renderAnnotations(login, clusterName, namespace),
				},
				Spec: corev1.PodSpec{
					Affinity:     nodeFilter.Affinity,
					NodeSelector: nodeFilter.NodeSelector,
					Tolerations:  nodeFilter.Tolerations,
					InitContainers: append(
						login.CustomInitContainers,
						common.RenderContainerMunge(&login.ContainerMunge),
					),
					Containers: []corev1.Container{
						renderContainerSshd(clusterType, &login.ContainerSshd, login.JailSubMounts, login.CustomVolumeMounts),
					},
					Volumes:   volumes,
					DNSPolicy: corev1.DNSClusterFirst,
					DNSConfig: &corev1.PodDNSConfig{
						Searches: []string{
							naming.BuildServiceFQDN(consts.ComponentTypeWorker, namespace, clusterName),
							naming.BuildLoginHeadlessServiceFQDN(namespace, clusterName),
						},
					},
					RestartPolicy:                 corev1.RestartPolicyAlways,
					TerminationGracePeriodSeconds: ptr.To(common.DefaultPodTerminationGracePeriodSeconds),
					SecurityContext:               &corev1.PodSecurityContext{},
					SchedulerName:                 corev1.DefaultSchedulerName,
					PriorityClassName:             login.PriorityClass,
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
