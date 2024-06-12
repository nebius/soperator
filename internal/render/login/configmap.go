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

// RenderConfigMapSSHConfigs renders new [corev1.ConfigMap] containing sshd config file
func RenderConfigMapSSHConfigs(cluster *values.SlurmCluster) (corev1.ConfigMap, error) {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSSHConfigsName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeLogin, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySshdConfig: generateSshdConfig(cluster).Render(),
		},
	}, nil
}

func generateSshdConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.RawConfig{}
	res.AddLine("LogLevel DEBUG3")
	res.AddLine(fmt.Sprintf("Port %d", cluster.NodeLogin.ContainerSshd.Port))
	res.AddLine("PermitRootLogin yes")
	res.AddLine("PasswordAuthentication no")
	res.AddLine("ChallengeResponseAuthentication no")
	res.AddLine("UsePAM yes")
	res.AddLine("AcceptEnv LANG LC_*")
	res.AddLine("X11Forwarding no")
	res.AddLine("AllowTcpForwarding no")
	res.AddLine("Subsystem sftp /usr/lib/openssh/sftp-server")
	res.AddLine("Match User *")
	res.AddLine("    ChrootDirectory " + consts.VolumeMountPathJail)
	return res
}
