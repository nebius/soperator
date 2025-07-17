package worker

import (
	"fmt"
	"maps"

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

// RenderStatefulSet renders new [kruisev1b1.StatefulSet] containing Slurm worker pods
func RenderStatefulSet(
	namespace,
	clusterName string,
	clusterType consts.ClusterType,
	nodeFilters []slurmv1.K8sNodeFilter,
	secrets *slurmv1.Secrets,
	volumeSources []slurmv1.VolumeSource,
	worker *values.SlurmWorker,
	workerFeatures []slurmv1.WorkerFeature,
) (kruisev1b1.StatefulSet, error) {
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
		return kruisev1b1.StatefulSet{}, fmt.Errorf("rendering volumes and claim template specs: %w", err)
	}

	// Since 1.29 is native sidecar support, we can use the native restart policy
	initContainers := []corev1.Container{
		common.RenderContainerMunge(&worker.ContainerMunge),
	}
	if worker.WaitForController != nil && *worker.WaitForController {
		initContainers = append(initContainers, RenderContainerWaitForController(&worker.ContainerSlurmd, clusterName))
	}
	if clusterType == consts.ClusterTypeGPU {
		initContainers = append(initContainers, renderContainerToolkitValidation(&worker.ContainerToolkitValidation))
	}
	initContainers = append(initContainers, worker.CustomInitContainers...)

	slurmdContainer, err := renderContainerSlurmd(
		&worker.ContainerSlurmd,
		worker.JailSubMounts,
		worker.CustomVolumeMounts,
		clusterName,
		clusterType,
		worker.CgroupVersion,
		worker.EnableGDRCopy,
		worker.SlurmNodeExtra,
		workerFeatures,
	)
	if err != nil {
		return kruisev1b1.StatefulSet{}, fmt.Errorf("rendering slurmd container: %w", err)
	}

	replicas := &worker.StatefulSet.Replicas
	if check.IsMaintenanceActive(worker.Maintenance) {
		replicas = ptr.To(consts.ZeroReplicas)
	}

	spec := corev1.PodSpec{
		PriorityClassName:  worker.PriorityClass,
		ServiceAccountName: naming.BuildServiceAccountWorkerName(clusterName),
		Affinity:           nodeFilter.Affinity,
		NodeSelector:       nodeFilter.NodeSelector,
		Tolerations:        nodeFilter.Tolerations,
		InitContainers:     initContainers,
		Containers: []corev1.Container{
			slurmdContainer,
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
	}

	if worker.PriorityClass != "" {
		spec.PriorityClassName = worker.PriorityClass
	}

	return kruisev1b1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      worker.StatefulSet.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: kruisev1b1.StatefulSetSpec{
			PodManagementPolicy: consts.PodManagementPolicy,
			ServiceName:         worker.Service.Name,
			Replicas:            replicas,
			UpdateStrategy: kruisev1b1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &kruisev1b1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable:  &worker.StatefulSet.MaxUnavailable,
					PodUpdatePolicy: kruisev1b1.RecreatePodUpdateStrategyType,
					Partition:       ptr.To(int32(0)),
					MinReadySeconds: ptr.To(int32(0)),
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
			VolumeClaimUpdateStrategy: kruisev1b1.VolumeClaimUpdateStrategy{
				Type: kruisev1b1.OnPodRollingUpdateVolumeClaimUpdateStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: renderAnnotations(worker, clusterName, namespace),
				},
				Spec: spec,
			},
			PersistentVolumeClaimRetentionPolicy: &kruisev1b1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType,
				WhenScaled:  kruisev1b1.RetainPersistentVolumeClaimRetentionPolicyType,
			},
		},
	}, nil
}

func renderAnnotations(worker *values.SlurmWorker, clusterName, namespace string) map[string]string {
	mungeAppArmorProfile := worker.ContainerMunge.AppArmorProfile
	workerAppArmorProfile := worker.ContainerSlurmd.AppArmorProfile

	if worker.UseDefaultAppArmorProfile {
		workerAppArmorProfile = fmt.Sprintf("%s/%s", "localhost", naming.BuildAppArmorProfileName(clusterName, namespace))
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

	maps.Copy(annotations, worker.WorkerAnnotations)

	return annotations
}
