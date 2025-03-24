package sconfigcontroller

import (
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderDeployment(
	clusterNamespace string,
	clusterName string,
	slurmAPISerer string,
	sConfigController *values.SConfigController,
	nodeFilter []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
) (deployment *appsv1.Deployment, err error) {
	labels := common.RenderLabels(consts.ComponentTypeSConfigController, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeSConfigController, clusterName)

	podTemplate, err := BasePodTemplateSpec(
		clusterNamespace,
		clusterName,
		slurmAPISerer,
		sConfigController,
		nodeFilter,
		volumeSources,
		matchLabels,
	)
	if err != nil {
		return nil, err
	}

	replicas := &sConfigController.Size

	if check.IsMaintenanceActive(&sConfigController.Maintenance) {
		replicas = ptr.To(consts.ZeroReplicas)
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildDeploymentName(consts.ComponentTypeSConfigController),
			Namespace: clusterNamespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: *podTemplate,
		},
	}, nil
}
