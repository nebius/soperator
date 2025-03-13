package login

import (
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
func RenderSshRootPublicKeysConfig(cluster *values.SlurmCluster) (corev1.ConfigMap, error) {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSshRootPublicKeysName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeLogin, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapKeySshRootPublicKeysConfig: generateSshRootPublicKeysConfig(cluster).Render(),
		},
	}, nil
}

func generateSshRootPublicKeysConfig(cluster *values.SlurmCluster) renderutils.ConfigFile {
	res := &renderutils.MultilineStringConfig{}
	for _, key := range cluster.NodeLogin.SSHRootPublicKeys {
		res.AddLine(key)
	}
	return res
}

// endregion SSHRootPublicKeys config

// region Supervisord

// RenderDefaultConfigMapSupervisord renders new [corev1.ConfigMap] containing supervisord config file
func RenderDefaultConfigMapSupervisord(cluster *values.SlurmCluster) corev1.ConfigMap {
	data := generateDefaultSupervisordConfig().Render()
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.NodeLogin.SupervisordConfigMapName,
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeLogin, cluster.Name),
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
