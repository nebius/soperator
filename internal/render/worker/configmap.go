package worker

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// region NCCL topology

// RenderConfigMapNCCLTopology renders new [corev1.ConfigMap] containing NCCL topology config file
func RenderConfigMapNCCLTopology(cluster *values.SlurmCluster) (corev1.ConfigMap, error) {
	ncclType, err := consts.StringToNCCLType(cluster.NodeWorker.NCCLSettings.TopologyType)
	if err != nil {
		return corev1.ConfigMap{}, err
	}
	topology, err := generateVirtualTopology(
		ncclType,
		cluster.NodeWorker.NCCLSettings.TopologyData,
	)
	if err != nil {
		return corev1.ConfigMap{}, err
	}

	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapNCCLTopologyName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeWorker, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeyNCCLTopology: topology.Render(),
		},
	}, nil
}

func generateVirtualTopology(ncclType consts.NCCLType, topologyData string) (renderutils.ConfigFile, error) {
	res := &renderutils.MultilineStringConfig{}
	switch ncclType {
	case consts.NCCLTypeAuto:
		return res, nil
	case consts.NCCLTypeCustom:
		if topologyData != "" {
			return renderutils.NewAsIsConfig(topologyData), nil
		}
		return res, errors.New("topologyData can't be empty for custom type of NCCL topology")
	default:
		return res, nil
	}
}

// endregion NCCL topology

// region Sysctl

// RenderConfigMapSysctl renders new [corev1.ConfigMap] containing sysctl config file
func RenderConfigMapSysctl(cluster *values.SlurmCluster) (corev1.ConfigMap, error) {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSysctlName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeWorker, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySysctl: generateSysctlConfig().Render(),
		},
	}, nil
}

func generateSysctlConfig() renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}
	res.AddProperty("vm.max_map_count", 655300)
	return res
}

// endregion Sysctl

// region Supervisord

// RenderDefaultConfigMapSupervisord renders new [corev1.ConfigMap] containing supervisord config file
func RenderDefaultConfigMapSupervisord(cluster *values.SlurmCluster) corev1.ConfigMap {
	data := generateDefaultSupervisordConfig().Render()
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.NodeWorker.SupervisordConfigMapName,
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeWorker, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySupervisord: data,
		},
	}
}

func generateDefaultSupervisordConfig() renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	res.AddLine("[supervisord]")
	res.AddLine("nodaemon=true")
	res.AddLine("logfile=/dev/null ; Output only to stdout/stderr")
	res.AddLine("logfile_maxbytes=0")
	res.AddLine("pidfile=/var/run/supervisord.pid")
	res.AddLine("")
	res.AddLine("[program:slurmd]")
	res.AddLine("priority=1")
	res.AddLine("stdout_logfile=/dev/fd/1")
	res.AddLine("stdout_logfile_maxbytes=0")
	res.AddLine("stderr_logfile=/dev/fd/2")
	res.AddLine("stderr_logfile_maxbytes=0")
	res.AddLine("redirect_stderr=true")
	res.AddLine("command=/opt/bin/slurm/slurmd_entrypoint.sh")
	res.AddLine("autostart=true")
	res.AddLine("autorestart=true")
	res.AddLine("startsecs=0")
	res.AddLine("stopasgroup=true ; Send SIGTERM to all child processes of supervisord")
	res.AddLine("killasgroup=true ; Send SIGKILL to all child processes of supervisord")
	res.AddLine("stopsignal=SIGTERM ; Signal to send to the program to stop it")
	res.AddLine("stopwaitsecs=10 ; Wait for the process to stop before sending a SIGKILL")
	res.AddLine("")
	res.AddLine("[program:sshd]")
	res.AddLine("priority=10")
	res.AddLine("stdout_logfile=/dev/fd/1")
	res.AddLine("stdout_logfile_maxbytes=0")
	res.AddLine("stderr_logfile=/dev/fd/2")
	res.AddLine("stderr_logfile_maxbytes=0")
	res.AddLine("redirect_stderr=true")
	res.AddLine("command=/usr/sbin/sshd -D -e -f /mnt/ssh-configs/sshd_config")
	res.AddLine("autostart=true")
	res.AddLine("autorestart=true")
	res.AddLine("startsecs=0")
	res.AddLine("stopasgroup=true ; Send SIGTERM to all child processes of supervisord")
	res.AddLine("killasgroup=true ; Send SIGKILL to all child processes of supervisord")
	res.AddLine("stopsignal=SIGTERM ; Signal to send to the program to stop it")
	res.AddLine("stopwaitsecs=10 ; Wait for the process to stop before sending a SIGKILL")

	return res
}

// endregion Supervisord

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
