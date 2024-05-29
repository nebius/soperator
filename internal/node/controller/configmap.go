package controller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/data/config"
	"nebius.ai/slurm-operator/internal/models/k8s"
	"nebius.ai/slurm-operator/internal/models/slurm"
)

// RenderConfigMap renders new [corev1.ConfigMap] containing '.conf' files for the following components:
//
// - [consts.ConfigMapSlurmConfigKey]: Slurm config
//
// - [consts.ConfigMapCGroupConfigKey]: cgroup config
//
// - [consts.ConfigMapSpankConfigKey]: SPANK plugins config
func RenderConfigMap(cluster smodels.ClusterValues) corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      consts.ConfigMapSlurmConfigsName,
			Namespace: cluster.Namespace,
			Labels:    k8smodels.BuildClusterDefaultLabels(cluster.Name, consts.ComponentTypeController),
		},
		Data: map[string]string{
			consts.ConfigMapSlurmConfigKey:  config.GenerateSlurmConfig(cluster.Controller.Service, cluster.Name).Render(),
			consts.ConfigMapCGroupConfigKey: config.GenerateCGroupConfig().Render(),
			consts.ConfigMapSpankConfigKey:  config.GenerateSpankConfig().Render(),
		},
	}
}
