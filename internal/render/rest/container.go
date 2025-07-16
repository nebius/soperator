package rest

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerREST renders [corev1.Container] for slurmrestd
func renderContainerREST(containerParams values.Container, threadCount *int32, maxConnections *int32) corev1.Container {
	if containerParams.Port == 0 {
		containerParams.Port = consts.DefaultRESTPort
	}
	containerParams.NodeContainer.Resources.Storage()

	var env []corev1.EnvVar
	if threadCount != nil {
		env = append(env, corev1.EnvVar{
			Name:  "SLURMRESTD_THREAD_COUNT",
			Value: strconv.Itoa(int(*threadCount)),
		})
	}
	if maxConnections != nil {
		env = append(env, corev1.EnvVar{
			Name:  "SLURMRESTD_MAX_CONNECTIONS",
			Value: strconv.Itoa(int(*maxConnections)),
		})
	}

	return corev1.Container{
		Name:            consts.ContainerNameREST,
		Image:           containerParams.Image,
		ImagePullPolicy: containerParams.ImagePullPolicy,
		Command:         containerParams.Command,
		Args:            containerParams.Args,
		Env:             env,
		Ports: []corev1.ContainerPort{{
			Name:          containerParams.Name,
			ContainerPort: containerParams.Port,
			Protocol:      corev1.ProtocolTCP,
		}},
		VolumeMounts: []corev1.VolumeMount{
			common.RenderVolumeMountSlurmConfigs(),
			common.RenderVolumeMountJailReadOnly(),
		},
		// TODO: Http check?
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(containerParams.Port),
				},
			},
			FailureThreshold:    5,
			InitialDelaySeconds: 15,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			TimeoutSeconds:      1,
		},
		SecurityContext: &corev1.SecurityContext{},
		Resources: corev1.ResourceRequirements{
			Limits:   common.CopyNonCPUResources(containerParams.Resources),
			Requests: containerParams.Resources,
		},
	}
}
