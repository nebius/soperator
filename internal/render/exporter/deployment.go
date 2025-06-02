package exporter

import (
	"errors"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderDeploymentExporter(clusterValues *values.SlurmCluster) (deployment *appsv1.Deployment, err error) {
	if !clusterValues.SlurmExporter.Enabled {
		return nil, errors.New("exporter is not enabled")
	}
	if clusterValues.SlurmExporter.Container.Image == "" {
		return nil, errors.New("image for ExporterContainer is empty")
	}

	labels := common.RenderLabels(consts.ComponentTypeExporter, clusterValues.Name)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeExporter, clusterValues.Name)

	replicas := 1
	if check.IsMaintenanceActive(clusterValues.SlurmExporter.Maintenance) {
		replicas = 0
	}

	// Hack to overcome apparmor labels validation during cluster upgrade reconciliation.
	// Otherwise Kubernetes API will not validate new deployment:
	// > Deployment.apps "exporter" is invalid:
	// > spec.template.annotations[container.apparmor.security.beta.kubernetes.io/munge]:
	// > Invalid value: "munge": container not found
	// TODO: remove in 2026.
	minNoopContainer, err := renderMinimalNoopContainer()
	if err != nil {
		return nil, err
	}
	initContainers := append(clusterValues.SlurmExporter.CustomInitContainers, minNoopContainer)

	podTemplateSpec := renderPodTemplateSpec(
		clusterValues,
		initContainers,
		matchLabels,
	)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildDeploymentName(consts.ComponentTypeExporter),
			Namespace: clusterValues.Namespace,
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
			Template: podTemplateSpec,
		},
	}, nil
}

func renderMinimalNoopContainer() (corev1.Container, error) {
	cpuResource, err := resource.ParseQuantity("20m")
	if err != nil {
		return corev1.Container{}, err
	}
	memoryResource, err := resource.ParseQuantity("20Mi")
	if err != nil {
		return corev1.Container{}, err
	}

	const minimalMultiArchImage = "busybox:latest"

	return corev1.Container{
		Name:            consts.ContainerNameMunge,
		Image:           minimalMultiArchImage,
		Command:         []string{"sleep"},
		Args:            []string{"0s"},
		ImagePullPolicy: corev1.PullIfNotPresent,
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    cpuResource,
				corev1.ResourceMemory: memoryResource,
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    cpuResource,
				corev1.ResourceMemory: memoryResource,
			},
		},
	}, nil
}
