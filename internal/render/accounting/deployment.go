package accounting

import (
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	consts "nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderDeployment(
	namespace,
	clusterName string,
	accounting *values.SlurmAccounting,
	nodeFilter []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
) (deployment *appsv1.Deployment, err error) {
	labels := common.RenderLabels(consts.ComponentTypeAccounting, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeAccounting, clusterName)

	podTemplate, err := BasePodTemplateSpec(
		namespace,
		clusterName,
		accounting,
		nodeFilter,
		volumeSources,
		matchLabels,
	)
	if err != nil {
		return nil, err
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildDeploymentName(consts.ComponentTypeAccounting, clusterName),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			// in Deployment mode replicas should be 1.
			// Because of accounting requires a single instance.
			Replicas: &accounting.Deployment.Replicas,
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
