package sconfigcontroller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
)

func RenderServiceAccount(clusterNamespace, clusterName string) corev1.ServiceAccount {
	labels := common.RenderLabels(consts.ComponentTypeWorker, clusterName)

	return corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildServiceAccountSconfigControllerName(clusterName),
			Namespace: clusterNamespace,
			Labels:    labels,
		},
	}
}
