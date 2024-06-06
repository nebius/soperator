package common

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderConfigMapSlurmConfigs renders new [corev1.ConfigMap] containing '.conf' files for the following components:
//
// [consts.ConfigMapSlurmConfigKey] - Slurm config
// [consts.ConfigMapCGroupConfigKey] - cgroup config
// [consts.ConfigMapSpankConfigKey] - SPANK plugins config
func RenderConfigMapSlurmConfigs(cluster *values.SlurmCluster) (corev1.ConfigMap, error) {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.ConfigMapSlurmConfigs.Name,
			Namespace: cluster.Namespace,
			Labels:    RenderLabels(consts.ComponentTypeController, cluster.Name),
		},
		Data: map[string]string{
			consts.ConfigMapSlurmConfigKey:  GenerateSlurmConfig(cluster).Render(),
			consts.ConfigMapCGroupConfigKey: GenerateCGroupConfig().Render(),
			consts.ConfigMapSpankConfigKey:  GenerateSpankConfig().Render(),
			consts.ConfigMapGresConfigKey:   GenerateGresConfig().Render(),
		},
	}, nil
}
