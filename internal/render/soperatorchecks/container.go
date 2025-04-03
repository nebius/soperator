package soperatorchecks

import (
	corev1 "k8s.io/api/core/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

func renderContainerK8sCronjob(check *slurmv1alpha1.ActiveCheck) corev1.Container {
	return corev1.Container{
		Name:            check.Spec.Name,
		Image:           check.Spec.K8sJobSpec.Image,
		Command:         check.Spec.K8sJobSpec.Command,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             check.Spec.K8sJobSpec.Env,
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				},
			},
		},
		VolumeMounts: check.Spec.K8sJobSpec.VolumeMounts,
	}
}
