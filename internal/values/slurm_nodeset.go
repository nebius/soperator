package values

import (
	"maps"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
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
	ContainerSSSD             *Container
	CustomInitContainers      []corev1.Container
	AppArmorProfileUseDefault bool

	SupervisorDConfigMapDefault bool
	SupervisorDConfigMapName    string

	SSHDConfigMapDefault    bool
	SSHDConfigMapName       string
	IsSSSDSecretDefault     bool
	SSSDConfSecretName      string
	SSSDLdapCAConfigMapName string

	GPU *slurmv1alpha1.GPUSpec

	StatefulSet     StatefulSet
	Service         Service
	ServiceUmbrella Service

	VolumeSpool                          corev1.VolumeSource
	VolumeJail                           corev1.VolumeSource
	JailSubMounts                        []slurmv1alpha1.NodeVolumeMount
	CustomVolumeMounts                   []slurmv1alpha1.NodeVolumeMount
	SharedMemorySize                     *resource.Quantity
	PersistentVolumeClaimRetentionPolicy *kruisev1b1.StatefulSetPersistentVolumeClaimRetentionPolicy

	Maintenance                  *consts.MaintenanceMode
	NodeExtra                    string
	EnableHostUserNamespace      bool
	WorkerInitRandomDelaySeconds int32

	EphemeralNodes               *bool
	EphemeralTopologyWaitTimeout int32

	// TopologyFabric is the IB fabric / top-of-tree switch name (spec.topology.fabric). It is
	// passed to worker-init so the dynamic topology path it declares matches the operator's
	// per-fabric root switch in topology.conf.
	TopologyFabric string

	ActiveNodes []int32
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
				CustomEnv:            nsSpec.Slurmd.CustomEnv,
				Image:                nsSpec.Slurmd.Image.GetURI(),
				ImagePullPolicy:      nsSpec.Slurmd.Image.PullPolicy,
				Port:                 nsSpec.Slurmd.Port,
				Resources:            nsSpec.Slurmd.Resources.DeepCopy(),
				LivenessProbe:        nsSpec.Slurmd.LivenessProbe,
				ReadinessProbe:       nsSpec.Slurmd.ReadinessProbe,
				SecurityLimitsConfig: nsSpec.Slurmd.Security.LimitsConfig,
				AppArmorProfile:      nsSpec.Slurmd.Security.AppArmorProfile,
				ProcMount:            nsSpec.Slurmd.Security.ProcMount,
			},
			consts.ContainerNameSlurmd,
		),
		ContainerMunge: buildContainerFrom(
			slurmv1.NodeContainer{
				Image:                nsSpec.Munge.Image.GetURI(),
				ImagePullPolicy:      nsSpec.Munge.Image.PullPolicy,
				Resources:            nsSpec.Munge.Resources.DeepCopy(),
				LivenessProbe:        nsSpec.Munge.LivenessProbe,
				ReadinessProbe:       nsSpec.Munge.ReadinessProbe,
				SecurityLimitsConfig: nsSpec.Munge.Security.LimitsConfig,
				AppArmorProfile:      nsSpec.Munge.Security.AppArmorProfile,
			},
			consts.ContainerNameMunge,
		),
		CustomInitContainers:      slices.Clone(nsSpec.CustomInitContainers),
		AppArmorProfileUseDefault: useDefaultAppArmorProfile,
		//
		GPU: nsSpec.GPU.DeepCopy(),
		//
		StatefulSet: buildStatefulSetWithMaxUnavailableFrom(
			naming.BuildNodeSetStatefulSetName(nodeSet.Name),
			nsSpec.Replicas,
			nsSpec.MaxUnavailable,
			nsSpec.MaxConcurrentStartup,
		),
		Service:         buildServiceFrom(naming.BuildNodeSetServiceName(clusterName, nodeSet.Name)),
		ServiceUmbrella: buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeNodeSet, clusterName)),
		//
		VolumeSpool:      *nsSpec.Slurmd.Volumes.Spool.DeepCopy(),
		VolumeJail:       *nsSpec.Slurmd.Volumes.Jail.DeepCopy(),
		SharedMemorySize: nsSpec.Slurmd.Volumes.SharedMemorySize,
		PersistentVolumeClaimRetentionPolicy: defaultPersistentVolumeClaimRetentionPolicy(
			nsSpec.Slurmd.Volumes.PersistentVolumeClaimRetentionPolicy,
		),
		//
		Maintenance:                  maintenance,
		NodeExtra:                    nsSpec.NodeConfig.Dynamic,
		EnableHostUserNamespace:      nsSpec.EnableHostUserNamespace,
		WorkerInitRandomDelaySeconds: nsSpec.WorkerInitRandomDelaySeconds,
		//
		EphemeralNodes:               nsSpec.EphemeralNodes,
		EphemeralTopologyWaitTimeout: nsSpec.EphemeralTopologyWaitTimeout,
		TopologyFabric:               nsSpec.Topology.Fabric,
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

	// region SSSDSecret
	{
		var (
			sssdSecretName = nsSpec.SSSDConfSecretRefName
			sssdDefault    = false
		)
		if nsSpec.SSSD != nil {
			if sssdSecretName == "" {
				sssdDefault = true
				sssdSecretName = naming.BuildNodeSetSecretSSSDConfName(clusterName, nodeSet.Name)
			}
			res.SSSDConfSecretName = sssdSecretName
			res.SSSDLdapCAConfigMapName = nsSpec.SSSDLdapCAConfigMapRefName
			res.IsSSSDSecretDefault = sssdDefault

			containerSSSD := buildContainerFrom(
				slurmv1.NodeContainer{
					Image:                nsSpec.SSSD.Image.GetURI(),
					ImagePullPolicy:      nsSpec.SSSD.Image.PullPolicy,
					Resources:            nsSpec.SSSD.Resources.DeepCopy(),
					LivenessProbe:        nsSpec.SSSD.LivenessProbe,
					ReadinessProbe:       nsSpec.SSSD.ReadinessProbe,
					SecurityLimitsConfig: nsSpec.SSSD.Security.LimitsConfig,
					AppArmorProfile:      nsSpec.SSSD.Security.AppArmorProfile,
				},
				consts.ContainerNameSSSD,
			)
			containerSSSD.SSSDDebugLevel = nsSpec.SSSDDebugLevel
			res.ContainerSSSD = &containerSSSD
		}
	}
	// endregion SSSDSecret

	return res
}

func defaultPersistentVolumeClaimRetentionPolicy(
	explicit *slurmv1alpha1.PersistentVolumeClaimRetentionPolicy,
) *kruisev1b1.StatefulSetPersistentVolumeClaimRetentionPolicy {
	res := &kruisev1b1.StatefulSetPersistentVolumeClaimRetentionPolicy{
		WhenDeleted: kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType,
		WhenScaled:  kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType,
	}
	if explicit == nil {
		return res
	}
	if explicit.WhenDeleted != "" {
		res.WhenDeleted = kruisev1b1.PersistentVolumeClaimRetentionPolicyType(explicit.WhenDeleted)
	}
	if explicit.WhenScaled != "" {
		res.WhenScaled = kruisev1b1.PersistentVolumeClaimRetentionPolicyType(explicit.WhenScaled)
	}
	return res
}
