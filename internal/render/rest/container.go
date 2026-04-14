package rest

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerREST renders [corev1.Container] for slurmrestd
func renderContainerREST(values *values.SlurmREST) corev1.Container {
	if values.ContainerREST.Port == 0 {
		values.ContainerREST.Port = consts.DefaultRESTPort
	}
	values.ContainerREST.NodeContainer.Resources.Storage()

	var env []corev1.EnvVar
	if values.ThreadCount != nil {
		env = append(env, corev1.EnvVar{
			Name:  "SLURMRESTD_THREAD_COUNT",
			Value: strconv.Itoa(int(*values.ThreadCount)),
		})
	}
	if values.MaxConnections != nil {
		env = append(env, corev1.EnvVar{
			Name:  "SLURMRESTD_MAX_CONNECTIONS",
			Value: strconv.Itoa(int(*values.MaxConnections)),
		})
	}
	env = append(env, values.ContainerREST.CustomEnv...)

	return corev1.Container{
		Name:            consts.ContainerNameREST,
		Image:           values.ContainerREST.Image,
		ImagePullPolicy: values.ContainerREST.ImagePullPolicy,
		Command:         values.ContainerREST.Command,
		Args:            values.ContainerREST.Args,
		Env:             env,
		Ports: []corev1.ContainerPort{{
			Name:          values.ContainerREST.Name,
			ContainerPort: values.ContainerREST.Port,
			Protocol:      corev1.ProtocolTCP,
		}},
		VolumeMounts: []corev1.VolumeMount{
			common.RenderVolumeMountSlurmConfigs(),
			common.RenderVolumeMountJailReadOnly(),
		},
		LivenessProbe:  values.ContainerREST.LivenessProbe,
		ReadinessProbe: values.ContainerREST.ReadinessProbe,
		SecurityContext: &corev1.SecurityContext{
			AppArmorProfile: common.ParseAppArmorProfile(values.ContainerREST.AppArmorProfile),
		},
		Resources: corev1.ResourceRequirements{
			Limits:   common.CopyNonCPUResources(values.ContainerREST.Resources),
			Requests: values.ContainerREST.Resources,
		},
	}
}
