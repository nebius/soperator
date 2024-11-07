package prometheus

import (
	"errors"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderDeploymentExporter(
	clusterName,
	namespace string,
	valuesExporter *values.SlurmExporter,
	nodeFilter []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
	podTemplate *corev1.PodTemplate,
) (deployment *appsv1.Deployment, err error) {
	if valuesExporter == nil || !valuesExporter.Enabled {
		return nil, errors.New("prometheus is not enabled")
	}
	if valuesExporter.ExporterContainer.Image == "" {
		return nil, errors.New("image for ContainerExporter is empty")
	}

	var podTemplateSpec *corev1.PodTemplateSpec = nil
	if podTemplate != nil {
		podTemplateSpec = &podTemplate.Template
	}

	// in Deployment mode replicas should be 1.
	// in StatefulSet mode replicas should be more than 1.
	// Because of munge container use pvcs, we can't use template pvc for munge in Deployment mode.
	replicas := int32(1)

	labels := common.RenderLabels(consts.ComponentTypeExporter, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeExporter, clusterName)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildDeploymentName(consts.ComponentTypeExporter),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: RenderPodTemplateSpec(
				clusterName,
				&valuesExporter.ContainerMunge,
				valuesExporter,
				nodeFilter,
				volumeSources,
				matchLabels,
				podTemplateSpec,
			),
		},
	}, nil
}
