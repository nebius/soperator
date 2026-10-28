package common

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func generateNamespaceConfig() renderutils.ConfigFile {
	return renderutils.NewAsIsConfig(`defaults:
  auto_base_path: false
  base_path: /tmp
  shared: true
  dir_confs:
    - path: /mnt/jail/dev/shm
      tmpfs: true
      options: "mode=1777"
    - path: /mnt/jail/tmp
      tmpfs: true
      options: "mode=1777"
    - path: /mnt/jail/mnt/memory
      tmpfs: true
      options: "mode=1777"`)
}

// RenderJailedConfigNamespace renders the generated or custom namespace configuration source.
func RenderJailedConfigNamespace(cluster *values.SlurmCluster) slurmv1alpha1.JailedConfig {
	configMapName := cluster.NamespaceConfig.ConfigMapName
	if configMapName == "" {
		configMapName = naming.BuildConfigMapSlurmConfigsName(cluster.Name)
	}
	labels := RenderLabels(consts.ComponentTypeController, cluster.Name)
	labels[consts.LabelJailedAggregationKey] = consts.LabelJailedAggregationCommonValue
	return slurmv1alpha1.JailedConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildJailedConfigNamespaceName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: slurmv1alpha1.JailedConfigSpec{
			ConfigMap: slurmv1alpha1.ConfigMapReference{Name: configMapName},
			Items: []corev1.KeyToPath{
				{Key: consts.ConfigMapKeyNamespaceConfig, Path: "/etc/slurm/namespace.yaml"},
			},
			UpdateActions: []slurmv1alpha1.UpdateAction{slurmv1alpha1.UpdateActionReconfigure},
		},
	}
}
