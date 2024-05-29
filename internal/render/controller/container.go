package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerSlurmCtlD renders Slurm controller [corev1.Container] for slurmctld
func renderContainerSlurmCtlD(cluster *values.SlurmCluster) (corev1.Container, error) {
	resources := corev1.ResourceList{
		corev1.ResourceCPU:              cluster.NodeController.Service.Resources.CPU,
		corev1.ResourceMemory:           cluster.NodeController.Service.Resources.Memory,
		corev1.ResourceEphemeralStorage: cluster.NodeController.Service.Resources.EphemeralStorage,
	}

	volumeMounts := []corev1.VolumeMount{
		common.RenderVolumeMountSlurmKey(),
		common.RenderVolumeMountSlurmConfigs(),
		common.RenderVolumeMountUsers(),
	}
	{
		vmSpool, err := common.RenderVolumeMountSpool(consts.ComponentTypeController)
		if err != nil {
			return corev1.Container{}, err
		}
		volumeMounts = append(volumeMounts, vmSpool)
	}

	return corev1.Container{
		Name:            consts.ContainerControllerName,
		Image:           cluster.NodeController.Service.Image,
		ImagePullPolicy: corev1.PullAlways, // TODO use digest and set to corev1.PullIfNotPresent
		Ports: []corev1.ContainerPort{{
			Name:          consts.ServiceControllerName,
			ContainerPort: cluster.NodeController.Service.Port,
			Protocol:      cluster.NodeController.Service.Protocol,
		}},
		VolumeMounts: volumeMounts,
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(cluster.NodeController.Service.Port),
				},
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					"SYS_ADMIN",
				}}},
		Resources: corev1.ResourceRequirements{
			Requests: resources,
			Limits:   resources,
		},
	}, nil
}
