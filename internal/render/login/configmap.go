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
