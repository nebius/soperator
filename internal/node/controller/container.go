package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"nebius.ai/slurm-operator/internal/consts"
	smodels "nebius.ai/slurm-operator/internal/models/slurm"
)

// renderSlurmCtldContainer renders Slurm controller [corev1.Container]
func renderSlurmCtldContainer(cluster smodels.ClusterValues) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerControllerSlurmCtldName,
		Image:           cluster.Controller.Image.String(),
		ImagePullPolicy: corev1.PullAlways, // TODO use digest and set to corev1.PullIfNotPresent
		Ports: []corev1.ContainerPort{{
			Name:          consts.ServiceControllerClusterTargetPort,
			ContainerPort: consts.ServiceControllerClusterPort,
			Protocol:      consts.ServiceControllerClusterPortProtocol,
		}},
		VolumeMounts: []corev1.VolumeMount{
			renderVolumeMountSlurmKey(),
			renderVolumeMountSlurmConfigs(),
			renderVolumeMountSpool(),
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromString(consts.ServiceControllerClusterTargetPort),
				},
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: cluster.Controller.Resources,
			Limits:   cluster.Controller.Resources,
		},
	}
}
