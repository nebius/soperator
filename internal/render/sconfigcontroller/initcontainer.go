package sconfigcontroller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
)

func renderInitContainerSConfigController(jailConfigPath string) corev1.Container {
	// Create a copy of the container's limits and add non-CPU resources from Requests

	// restartPolicy := corev1.ContainerRestartPolicyAlways
	return corev1.Container{
		Name:            "init-dir",
		Image:           consts.InitContainerImageSconfigController,
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser: ptr.To(int64(0)),
		},
		VolumeMounts: []corev1.VolumeMount{
			common.RenderVolumeMountJail(),
		},
		Env: []corev1.EnvVar{
			{
				Name:  "JAIL_CONFIG_PATH",
				Value: jailConfigPath,
			},
		},
		Command: []string{"/bin/sh", "-c"}, // Use bash to execute the script
		Args: []string{
			// Quotes around variables are load-bearing, so shell would treat them as single string each
			"mkdir -p \"${JAIL_CONFIG_PATH}\" && chown 1001:1001 \"${JAIL_CONFIG_PATH}\" && chmod 755 \"${JAIL_CONFIG_PATH}\"",
		},
	}
}
