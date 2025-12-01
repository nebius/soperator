package worker

import (
	"fmt"
	"maps"

	appspub "github.com/openkruise/kruise-api/apps/pub"
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
	labels[consts.LabelWorkerKey] = consts.LabelWorkerValue
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
		common.RenderContainerMunge(&worker.ContainerMunge), RenderContainerWaitForController(&worker.ContainerSlurmd),
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
		HostUsers: worker.HostUsers,
		ReadinessGates: []corev1.PodReadinessGate{
			{
				ConditionType: appspub.InPlaceUpdateReady,
			},
		},
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
				naming.BuildServiceFQDN(worker.Service.Name, namespace),
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
					PodUpdatePolicy: kruisev1b1.InPlaceIfPossiblePodUpdateStrategyType,
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

// RenderNodeSetStatefulSet renders new [kruisev1b1.StatefulSet] containing NodeSet worker pods
func RenderNodeSetStatefulSet(
	nodeSet *values.SlurmNodeSet,
	secrets *slurmv1.Secrets,
) (kruisev1b1.StatefulSet, error) {
	labels := common.RenderLabels(consts.ComponentTypeNodeSet, nodeSet.ParentalCluster.Name)
	labels[consts.LabelNodeSetKey] = nodeSet.Name
	labels[consts.LabelWorkerKey] = consts.LabelWorkerValue
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeNodeSet, nodeSet.ParentalCluster.Name)
	matchLabels[consts.LabelNodeSetKey] = nodeSet.Name

	volumes, pvcTemplateSpecs, err := renderVolumesAndClaimTemplateSpecsForNodeSet(nodeSet, secrets)
	if err != nil {
		return kruisev1b1.StatefulSet{}, fmt.Errorf("rendering volumes and claim template specs: %w", err)
	}

	initContainers := []corev1.Container{
		common.RenderContainerMunge(&nodeSet.ContainerMunge),
		RenderContainerWaitForController(&nodeSet.ContainerSlurmd),
	}
	initContainers = append(initContainers, nodeSet.CustomInitContainers...)

	slurmdContainer, err := renderContainerNodeSetSlurmd(nodeSet)
	if err != nil {
		return kruisev1b1.StatefulSet{}, fmt.Errorf("rendering slurmd container: %w", err)
	}

	replicas := &nodeSet.StatefulSet.Replicas
	if check.IsMaintenanceActive(nodeSet.Maintenance) {
		replicas = ptr.To(consts.ZeroReplicas)
	}

	spec := corev1.PodSpec{
		HostUsers: &nodeSet.EnableHostUserNamespace,
		ReadinessGates: []corev1.PodReadinessGate{
			{
				ConditionType: appspub.InPlaceUpdateReady,
			},
		},
		PriorityClassName:  nodeSet.PriorityClass,
		ServiceAccountName: naming.BuildServiceAccountWorkerName(nodeSet.ParentalCluster.Name),
		Affinity:           nodeSet.Affinity,
		NodeSelector:       nodeSet.NodeSelector,
		Tolerations:        nodeSet.Tolerations,
		InitContainers:     initContainers,
		Containers: []corev1.Container{
			slurmdContainer,
		},
		Volumes:   volumes,
		DNSPolicy: corev1.DNSClusterFirst,
		DNSConfig: &corev1.PodDNSConfig{
			Searches: []string{
				naming.BuildServiceFQDN(nodeSet.ServiceUmbrella.Name, nodeSet.ParentalCluster.Namespace),
				naming.BuildLoginHeadlessServiceFQDN(nodeSet.ParentalCluster.Namespace, nodeSet.ParentalCluster.Name),
			},
		},
		RestartPolicy:                 corev1.RestartPolicyAlways,
		TerminationGracePeriodSeconds: ptr.To(common.DefaultPodTerminationGracePeriodSeconds),
		SecurityContext:               &corev1.PodSecurityContext{},
		SchedulerName:                 corev1.DefaultSchedulerName,
	}

	if nodeSet.PriorityClass != "" {
		spec.PriorityClassName = nodeSet.PriorityClass
	}

	return kruisev1b1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeSet.StatefulSet.Name,
			Namespace: nodeSet.ParentalCluster.Namespace,
			Labels:    labels,
		},
		Spec: kruisev1b1.StatefulSetSpec{
			PodManagementPolicy: consts.PodManagementPolicy,
			ServiceName:         nodeSet.Service.Name,
			Replicas:            replicas,
			UpdateStrategy: kruisev1b1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &kruisev1b1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable:  &nodeSet.StatefulSet.MaxUnavailable,
					PodUpdatePolicy: kruisev1b1.InPlaceIfPossiblePodUpdateStrategyType,
					Partition:       ptr.To(int32(0)),
					MinReadySeconds: ptr.To(int32(0)),
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			VolumeClaimTemplates: common.RenderVolumeClaimTemplates(
				consts.ComponentTypeNodeSet,
				nodeSet.ParentalCluster.Namespace,
				nodeSet.ParentalCluster.Name,
				pvcTemplateSpecs,
			),
			VolumeClaimUpdateStrategy: kruisev1b1.VolumeClaimUpdateStrategy{
				Type: kruisev1b1.OnPodRollingUpdateVolumeClaimUpdateStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: renderNodeSetAnnotations(nodeSet),
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
		consts.AnnotationDefaultContainerName: consts.ContainerNameSlurmd,
	}

	maps.Copy(annotations, worker.WorkerAnnotations)

	return annotations
}

func renderNodeSetAnnotations(nodeSet *values.SlurmNodeSet) map[string]string {
	mungeAppArmorProfile := nodeSet.ContainerMunge.AppArmorProfile
	workerAppArmorProfile := nodeSet.ContainerSlurmd.AppArmorProfile

	// TODO (@dstaroff) make use of defaulting webhook for this
	if mungeAppArmorProfile == "" {
		mungeAppArmorProfile = consts.AppArmorProfileUnconfined
	}
	if workerAppArmorProfile == "" {
		if nodeSet.AppArmorProfileUseDefault {
			workerAppArmorProfile = fmt.Sprintf("%s/%s", "localhost", naming.BuildAppArmorProfileName(nodeSet.ParentalCluster.Name, nodeSet.ParentalCluster.Namespace))
		} else {
			workerAppArmorProfile = consts.AppArmorProfileUnconfined
		}
	}

	annotations := map[string]string{
		fmt.Sprintf(
			"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameSlurmd,
		): workerAppArmorProfile,
		fmt.Sprintf(
			"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameMunge,
		): mungeAppArmorProfile,
		consts.AnnotationDefaultContainerName: consts.ContainerNameSlurmd,
	}

	maps.Copy(annotations, nodeSet.Annotations)

	return annotations
}
