package login

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// region SSHRootPublicKeys config

// RenderSshRootPublicKeysConfig renders new [corev1.ConfigMap] containing root public keys
func RenderSshRootPublicKeysConfig(cluster *values.SlurmCluster) corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSshRootPublicKeysName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeLogin, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySshRootPublicKeysConfig: generateSshRootPublicKeysConfig(cluster).Render(),
		},
	}
}

func generateSshRootPublicKeysConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	for _, key := range cluster.NodeLogin.SSHRootPublicKeys {
		res.AddLine(key)
	}
	return res
}

// endregion SSHRootPublicKeys config

// region SSHD config

// RenderConfigMapSSHDConfigs renders new [corev1.ConfigMap] containing sshd config file
func RenderConfigMapSSHDConfigs(
	cluster *values.SlurmCluster,
	componentType consts.ComponentType,
) corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSSHDConfigsNameLogin(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(componentType, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySshdConfig: generateSshdConfig(cluster).Render(),
		},
	}
}

func generateSshdConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine(fmt.Sprintf("Port %d", cluster.NodeLogin.ContainerSshd.Port))
	res.AddLine("PermitRootLogin yes")
	res.AddLine("PasswordAuthentication no")
	res.AddLine("ChallengeResponseAuthentication no")
	res.AddLine("UsePAM yes")
	res.AddLine("AcceptEnv LANG LC_*")
	res.AddLine("X11Forwarding no")
	res.AddLine("AllowTcpForwarding yes")
	res.AddLine("Subsystem sftp internal-sftp")
	res.AddLine("HostKey " + consts.VolumeMountPathSSHDKeys + "/" + consts.SecretSshdRSAKeyName)
	res.AddLine("HostKey " + consts.VolumeMountPathSSHDKeys + "/" + consts.SecretSshdECDSAKeyName)
	res.AddLine("HostKey " + consts.VolumeMountPathSSHDKeys + "/" + consts.SecretSshdECDSA25519KeyName)
	res.AddLine("ChrootDirectory " + consts.VolumeMountPathJail)
	res.AddLine("ClientAliveInterval " + consts.SSHDClientAliveInterval)
	res.AddLine("ClientAliveCountMax " + consts.SSHDClientAliveCountMax)
	res.AddLine("MaxStartups " + consts.SSHDMaxStartups)
	res.AddLine("LoginGraceTime " + consts.SSHDLoginGraceTime)
	res.AddLine("MaxAuthTries " + consts.SSHDMaxAuthTries)
	res.AddLine("LogLevel DEBUG3")
	res.AddLine("")
	res.AddLine("Match User root")
	res.AddLine("    AuthorizedKeysFile /root/.ssh/authorized_keys " + consts.VolumeMountPathJail + "/root/.ssh/authorized_keys")
	res.AddLine("")
	res.AddLine("Match User *")
	res.AddLine("    LogLevel INFO")
	return res
}

// endregion SSHD config
