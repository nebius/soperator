package worker

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

// region Sysctl

// RenderConfigMapSysctl renders new [corev1.ConfigMap] containing sysctl config file
func RenderConfigMapSysctl(cluster *values.SlurmCluster) corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSysctlName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeWorker, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySysctl: generateSysctlConfig().Render(),
		},
	}
}

func generateSysctlConfig() renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}
	res.AddProperty("vm.max_map_count", 655300)
	return res
}

// endregion Sysctl

// region SSHD config

// RenderConfigMapSSHDConfigs renders new [corev1.ConfigMap] containing sshd config file
func RenderConfigMapSSHDConfigs(
	cluster *values.SlurmCluster,
	componentType consts.ComponentType,
) corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSSHDConfigsNameWorker(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(componentType, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySshdConfig: generateSshdConfig(&cluster.NodeLogin).Render(),
		},
	}
}

func generateSshdConfig(login *values.SlurmLogin) renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine(fmt.Sprintf("Port %d", login.ContainerSshd.Port))
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
	res.AddLine("# Upgrade fallback for pre-PAM images; PAM-jail images remove this at startup.")
	res.AddLine("ChrootDirectory " + consts.VolumeMountPathJail)
	res.AddLine("ClientAliveInterval " + consts.SSHDClientAliveInterval)
	res.AddLine("ClientAliveCountMax " + consts.SSHDClientAliveCountMax)
	res.AddLine("MaxStartups " + consts.SSHDMaxStartups)
	res.AddLine("LoginGraceTime " + consts.SSHDLoginGraceTime)
	res.AddLine("MaxAuthTries " + consts.SSHDMaxAuthTries)
	res.AddLine("LogLevel DEBUG3")
	if login.ContainerSSSD != nil {
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

// region PAM Slurm adopt

// RenderConfigMapPAMSlurmAdopt renders the worker PAM account policy and its exemption list.
func RenderConfigMapPAMSlurmAdopt(cluster *values.SlurmCluster) corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapPAMSlurmAdoptName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeWorker, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeyPAMSlurmAdopt:       generatePAMSlurmAdoptConfig(cluster.PAMSlurmAdopt).Render(),
			consts.ConfigMapKeyPAMSlurmAdoptUsers:  generatePAMSlurmAdoptIdentityList(cluster.PAMSlurmAdopt.ExemptUsers).Render(),
			consts.ConfigMapKeyPAMSlurmAdoptGroups: generatePAMSlurmAdoptIdentityList(cluster.PAMSlurmAdopt.ExemptGroups).Render(),
		},
	}
}

func generatePAMSlurmAdoptConfig(config values.PAMSlurmAdopt) renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	if !config.Enabled {
		return res
	}

	res.AddLine(fmt.Sprintf(
		"account sufficient pam_listfile.so item=user sense=allow onerr=fail file=%s",
		consts.VolumeMountPathPAMSlurmAdoptUsers,
	))
	res.AddLine(fmt.Sprintf(
		"account sufficient pam_listfile.so item=group sense=allow onerr=fail file=%s",
		consts.VolumeMountPathPAMSlurmAdoptGroups,
	))
	res.AddLine("-account required pam_slurm_adopt.so action_no_jobs=deny action_unknown=newest action_adopt_failure=deny action_generic_failure=deny disable_x11=1 join_container=false")
	return res
}

func generatePAMSlurmAdoptIdentityList(identities []string) renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	for _, identity := range identities {
		res.AddLine(identity)
	}
	return res
}

// endregion PAM Slurm adopt
