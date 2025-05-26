package exporter

import (
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderDeploymentExporter(
	clusterName,
	namespace string,
	exporterValues values.SlurmExporter,
	nodeFilter []slurmv1.K8sNodeFilter,
	podTemplatePatch *corev1.PodTemplate,
	slurmAPIServer string,
) (deployment *appsv1.Deployment, err error) {
	if !exporterValues.Enabled {
		return nil, errors.New("exporter is not enabled")
	}
	if exporterValues.NodeContainer.Image == "" {
		return nil, errors.New("image for NodeContainer is empty")
	}

	var podTemplatePatchSpec *corev1.PodTemplateSpec
	if podTemplatePatch != nil {
		podTemplatePatchSpec = &podTemplatePatch.Template
	}

	labels := common.RenderLabels(consts.ComponentTypeExporter, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeExporter, clusterName)

	replicas := 1
	if check.IsMaintenanceActive(exporterValues.Maintenance) {
		replicas = 0
	}

	renderedPodTemplateSpec, err := renderPodTemplateSpec(
		clusterName,
		namespace,
		exporterValues.CustomInitContainers,
		exporterValues,
		nodeFilter,
		matchLabels,
		podTemplatePatchSpec,
		slurmAPIServer,
	)
	if err != nil {
		return nil, fmt.Errorf("render pod template spec: %w", err)
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildDeploymentName(consts.ComponentTypeExporter),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(replicas)),
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: renderedPodTemplateSpec,
		},
	}, nil
}
