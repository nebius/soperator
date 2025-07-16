package sconfigcontroller

import (
	"fmt"

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
		Command: []string{"/bin/sh", "-c"}, // Use bash to execute the script
		Args: []string{
			fmt.Sprintf(
				"mkdir -p %[1]s && chown 1001:1001 %[1]s && chmod 755 %[1]s",
				jailConfigPath,
			),
		},
	}
}
