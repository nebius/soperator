package values

import (
	corev1 "k8s.io/api/core/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmLogin contains the data needed to deploy and reconcile Slurm Login nodes
type SlurmLogin struct {
	slurmv1.SlurmNode

	ContainerSshd        Container
	ContainerMunge       Container
	CustomInitContainers []corev1.Container

	Service         Service
	HeadlessService Service
	StatefulSet     StatefulSet

	IsSSHDConfigMapDefault bool
	SSHDConfigMapName      string
	SSHRootPublicKeys      []string

	VolumeJail         slurmv1.NodeVolume
	JailSubMounts      []slurmv1.NodeVolumeMount
	CustomVolumeMounts []slurmv1.NodeVolumeMount

	UseDefaultAppArmorProfile bool
	Maintenance               *consts.MaintenanceMode
}

func buildSlurmLoginFrom(clusterName string, maintenance *consts.MaintenanceMode, login *slurmv1.SlurmNodeLogin, useDefaultAppArmorProfile bool) SlurmLogin {
	svc := buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeLogin, clusterName))
	svc.Type = login.SshdServiceType
	svc.Annotations = login.SshdServiceAnnotations
	svc.LoadBalancerIP = login.SshdServiceLoadBalancerIP
	svc.NodePort = login.SshdServiceNodePort

	headlessSvc := buildServiceFrom(naming.BuildLoginHeadlessServiceName(clusterName))
	headlessSvc.Type = corev1.ServiceTypeClusterIP

	sshdConfigMapName := login.SSHDConfigMapRefName
	isSSHDConfigDefault := sshdConfigMapName == ""
	if isSSHDConfigDefault {
		sshdConfigMapName = naming.BuildConfigMapSSHDConfigsNameLogin(clusterName)
	}

	res := SlurmLogin{
		SlurmNode: *login.SlurmNode.DeepCopy(),
		ContainerSshd: buildContainerFrom(
			login.Sshd,
			consts.ContainerNameSshd,
		),
		ContainerMunge: buildContainerFrom(
			login.Munge,
			consts.ContainerNameMunge,
		),
		CustomInitContainers: login.CustomInitContainers,
		Service:              svc,
		HeadlessService:      headlessSvc,
		StatefulSet: buildStatefulSetFrom(
			naming.BuildStatefulSetName(consts.ComponentTypeLogin),
			login.SlurmNode.Size,
		),
		SSHDConfigMapName:         sshdConfigMapName,
		IsSSHDConfigMapDefault:    isSSHDConfigDefault,
		SSHRootPublicKeys:         login.SshRootPublicKeys,
		VolumeJail:                *login.Volumes.Jail.DeepCopy(),
		UseDefaultAppArmorProfile: useDefaultAppArmorProfile,
		Maintenance:               maintenance,
	}
	for _, jailSubMount := range login.Volumes.JailSubMounts {
		subMount := *jailSubMount.DeepCopy()
		res.JailSubMounts = append(res.JailSubMounts, subMount)
	}
	for _, customVolumeMount := range login.Volumes.CustomMounts {
		customMount := *customVolumeMount.DeepCopy()
		res.CustomVolumeMounts = append(res.CustomVolumeMounts, customMount)
	}

	return res
}
