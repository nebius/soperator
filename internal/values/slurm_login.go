package values

import (
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmLogin contains the data needed to deploy and reconcile Slurm Login nodes
type SlurmLogin struct {
	slurmv1.SlurmNode

	ContainerSshd  Container
	ContainerMunge Container

	Service     Service
	StatefulSet StatefulSet

	SshRootPublicKeys []string

	VolumeJail    slurmv1.NodeVolume
	JailSubMounts []slurmv1.NodeVolumeJailSubMount
}

func buildSlurmLoginFrom(clusterName string, login *slurmv1.SlurmNodeLogin) SlurmLogin {
	svc := buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeLogin, clusterName))
	svc.Type = login.SshdServiceType
	svc.Annotations = login.SshdServiceAnnotations
	svc.LoadBalancerIP = login.SshdServiceLoadBalancerIP
	svc.NodePort = login.SshdServiceNodePort

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
		Service: svc,
		StatefulSet: buildStatefulSetFrom(
			naming.BuildStatefulSetName(consts.ComponentTypeLogin, clusterName),
			login.SlurmNode.Size,
		),
		SshRootPublicKeys: login.SshRootPublicKeys,
		VolumeJail:        *login.Volumes.Jail.DeepCopy(),
	}
	for _, jailSubMount := range login.Volumes.JailSubMounts {
		subMount := *jailSubMount.DeepCopy()
		res.JailSubMounts = append(res.JailSubMounts, subMount)
	}

	return res
}
