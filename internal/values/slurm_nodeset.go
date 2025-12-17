package values

import (
	"maps"

	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

type SlurmNodeSet struct {
	Name            string
	ParentalCluster client.ObjectKey

	NodeSelector  map[string]string
	Affinity      *corev1.Affinity
	Tolerations   []corev1.Toleration
	PriorityClass string
	Annotations   map[string]string

	ContainerSlurmd           Container
	ContainerMunge            Container
	CustomInitContainers      []corev1.Container
	CgroupVersion             string
	AppArmorProfileUseDefault bool

	SupervisorDConfigMapDefault bool
	SupervisorDConfigMapName    string

	SSHDConfigMapDefault bool
	SSHDConfigMapName    string

	GPU *slurmv1alpha1.GPUSpec

	StatefulSet     StatefulSet
	Service         Service
	ServiceUmbrella Service

	VolumeSpool        corev1.VolumeSource
	VolumeJail         corev1.VolumeSource
	JailSubMounts      []slurmv1alpha1.NodeVolumeMount
	CustomVolumeMounts []slurmv1alpha1.NodeVolumeMount
	SharedMemorySize   *resource.Quantity

	Maintenance             *consts.MaintenanceMode
	NodeExtra               string
	EnableHostUserNamespace bool
}

func BuildSlurmNodeSetFrom(
	nodeSet *slurmv1alpha1.NodeSet,
	clusterName string,
	maintenance *consts.MaintenanceMode,
	useDefaultAppArmorProfile bool,
) SlurmNodeSet {
	nsSpec := &nodeSet.Spec
	res := SlurmNodeSet{
		Name: nodeSet.Name,
		ParentalCluster: client.ObjectKey{
			Namespace: nodeSet.Namespace,
			Name:      clusterName,
		},
		//
		NodeSelector:  maps.Clone(nsSpec.NodeSelector),
		Affinity:      nsSpec.Affinity.DeepCopy(),
		Tolerations:   slices.Clone(nsSpec.Tolerations),
		PriorityClass: nsSpec.PriorityClass,
		Annotations:   maps.Clone(nsSpec.WorkerAnnotations),
		//
		ContainerSlurmd: buildContainerFrom(
			slurmv1.NodeContainer{
				Image:                nsSpec.Slurmd.Image.GetURI(),
				ImagePullPolicy:      nsSpec.Slurmd.Image.PullPolicy,
				Port:                 nsSpec.Slurmd.Port,
				Resources:            nsSpec.Slurmd.Resources.DeepCopy(),
				SecurityLimitsConfig: nsSpec.Slurmd.Security.LimitsConfig,
				AppArmorProfile:      nsSpec.Slurmd.Security.AppArmorProfile,
			},
			consts.ContainerNameSlurmd,
		),
		ContainerMunge: buildContainerFrom(
			slurmv1.NodeContainer{
				Image:                nsSpec.Munge.Image.GetURI(),
				ImagePullPolicy:      nsSpec.Munge.Image.PullPolicy,
				Resources:            nsSpec.Munge.Resources.DeepCopy(),
				SecurityLimitsConfig: nsSpec.Munge.Security.LimitsConfig,
				AppArmorProfile:      nsSpec.Munge.Security.AppArmorProfile,
			},
			consts.ContainerNameMunge,
		),
		CustomInitContainers:      slices.Clone(nsSpec.CustomInitContainers),
		CgroupVersion:             nsSpec.Slurmd.CgroupVersion,
		AppArmorProfileUseDefault: useDefaultAppArmorProfile,
		//
		GPU: nsSpec.GPU.DeepCopy(),
		//
		StatefulSet: buildStatefulSetWithMaxUnavailableFrom(
			naming.BuildNodeSetStatefulSetName(nodeSet.Name),
			nsSpec.Replicas,
			nsSpec.MaxUnavailable,
		),
		Service:         buildServiceFrom(naming.BuildNodeSetServiceName(clusterName, nodeSet.Name)),
		ServiceUmbrella: buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeNodeSet, clusterName)),
		//
		VolumeSpool:      *nsSpec.Slurmd.Volumes.Spool.DeepCopy(),
		VolumeJail:       *nsSpec.Slurmd.Volumes.Jail.DeepCopy(),
		SharedMemorySize: nsSpec.Slurmd.Volumes.SharedMemorySize,
		//
		Maintenance:             maintenance,
		NodeExtra:               nsSpec.NodeConfig.Dynamic,
		EnableHostUserNamespace: nsSpec.EnableHostUserNamespace,
	}

	// region Submounts
	for _, jailSubMount := range nsSpec.Slurmd.Volumes.JailSubMounts {
		subMount := *jailSubMount.DeepCopy()
		res.JailSubMounts = append(res.JailSubMounts, subMount)
	}
	for _, customVolumeMount := range nsSpec.Slurmd.Volumes.CustomVolumeMounts {
		customMount := *customVolumeMount.DeepCopy()
		res.CustomVolumeMounts = append(res.CustomVolumeMounts, customMount)
	}
	// endregion Submounts

	// region SupervisorDConfig
	{
		var (
			supervisordConfigMapName = nsSpec.ConfigMapRefSupervisord
			supervisordConfigDefault = false
		)
		if nsSpec.ConfigMapRefSupervisord == "" {
			supervisordConfigDefault = true
			supervisordConfigMapName = naming.BuildConfigMapSupervisordName(clusterName)
		}
		res.SupervisorDConfigMapName = supervisordConfigMapName
		res.SupervisorDConfigMapDefault = supervisordConfigDefault
	}
	// endregion SupervisorDConfig

	// region SSHDConfig
	{
		var (
			sshdConfigMapName = nsSpec.ConfigMapRefSSHD
			sshdConfigDefault = false
		)
		if nsSpec.ConfigMapRefSSHD == "" {
			sshdConfigDefault = true
			sshdConfigMapName = naming.BuildConfigMapSSHDConfigsNameWorker(clusterName)
		}
		res.SSHDConfigMapName = sshdConfigMapName
		res.SSHDConfigMapDefault = sshdConfigDefault
	}
	// endregion SSHDConfig

	return res
}
