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
	if cluster.NodeLogin.ContainerSSSD != nil {
		res.AddLine("")
		res.AddLine("# Ask SSSD for users' public keys")
		res.AddLine("AuthorizedKeysCommand /usr/bin/sss_ssh_authorizedkeys")
		res.AddLine("AuthorizedKeysCommandUser root")
	}
	res.AddLine("")
	res.AddLine("Match User root")
	res.AddLine("    AuthorizedKeysFile /root/.ssh/authorized_keys " + consts.VolumeMountPathJail + "/root/.ssh/authorized_keys")
	res.AddLine("")
	res.AddLine("Match User *")
	res.AddLine("    LogLevel INFO")
	return res
}

// endregion SSHD config

// region User isolation config

// RenderConfigMapUserIsolation renders new [corev1.ConfigMap] containing per-user cgroup
// isolation settings consumed by the login sshd entrypoint and the PAM session hook
func RenderConfigMapUserIsolation(cluster *values.SlurmCluster) corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapUserIsolationName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeLogin, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeyUserIsolation: generateUserIsolationConfig(cluster).Render(),
		},
	}
}

const (
	// Derived per-user memory defaults, applied when the corresponding limit is not set
	// explicitly. They are fractions of the sshd container memory limit, so they stay
	// right-sized on any login node size. memory.max below the container limit keeps the
	// OOM killer scoped to a single user's cgroup instead of the whole container.
	derivedMemoryHighPercent = 80
	derivedMemoryMaxPercent  = 90
)

func generateUserIsolationConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}
	res.AddComment(" Managed by soperator. Consumed by sshd_entrypoint.sh and the PAM session hook.")
	res.AddComment(" Memory values are in bytes (cgroup v2 memory.high / memory.max).")

	isolation := cluster.NodeLogin.UserIsolation
	if isolation == nil || !isolation.Enabled {
		res.AddProperty("SOPERATOR_USER_ISOLATION_ENABLED", false)
		return res
	}

	containerMemory := cluster.NodeLogin.ContainerSshd.Resources.Memory().Value()

	res.AddProperty("SOPERATOR_USER_ISOLATION_ENABLED", true)
	switch {
	case isolation.MemoryHigh != nil:
		res.AddProperty("SOPERATOR_USER_ISOLATION_MEMORY_HIGH", isolation.MemoryHigh.Value())
	case containerMemory > 0:
		res.AddProperty("SOPERATOR_USER_ISOLATION_MEMORY_HIGH", containerMemory*derivedMemoryHighPercent/100)
	}
	switch {
	case isolation.MemoryMax != nil:
		res.AddProperty("SOPERATOR_USER_ISOLATION_MEMORY_MAX", isolation.MemoryMax.Value())
	case containerMemory > 0:
		res.AddProperty("SOPERATOR_USER_ISOLATION_MEMORY_MAX", containerMemory*derivedMemoryMaxPercent/100)
	}
	cpuWeight := isolation.CPUWeight
	if cpuWeight == 0 {
		// The CRD defaults cpuWeight to 100 on admission; guard for objects built in code.
		cpuWeight = 100
	}
	res.AddProperty("SOPERATOR_USER_ISOLATION_CPU_WEIGHT", cpuWeight)
	return res
}

// endregion User isolation config
