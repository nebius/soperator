package soperatorchecks

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
)

// RenderSbatchConfigMap renders new [corev1.ConfigMap] containing slurm sbatch script
func RenderSbatchConfigMap(checkName, clusterName, namespace, script string) corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildConfigMapSbatchScriptName(checkName),
			Namespace: namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeSoperatorChecks, clusterName),
		},
		Data: map[string]string{
			consts.ConfigMapKeySoperatorcheckSbatch: script,
		},
	}
}
