package exporter

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
)

func RenderServiceAccount(clusterNamespace, clusterName string) corev1.ServiceAccount {
	labels := common.RenderLabels(consts.ComponentTypeExporter, clusterName)

	return corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceAccountName,
			Namespace: clusterNamespace,
			Labels:    labels,
		},
	}
}
