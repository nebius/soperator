package soperatorchecks

import (
	corev1 "k8s.io/api/core/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
)

func renderContainerK8sCronjob(check *slurmv1alpha1.ActiveCheck) corev1.Container {
	var container corev1.Container

	if check.Spec.CheckType == "k8sJob" {
		container = corev1.Container{
			Name:            check.Spec.Name,
			Image:           check.Spec.K8sJobSpec.JobContainer.Image,
			Command:         check.Spec.K8sJobSpec.JobContainer.Command,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Env:             check.Spec.K8sJobSpec.JobContainer.Env,
			SecurityContext: &corev1.SecurityContext{
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{consts.ContainerSecurityContextCapabilitySysAdmin},
				},
			},
			VolumeMounts: check.Spec.K8sJobSpec.JobContainer.VolumeMounts,
		}

		if check.Spec.K8sJobSpec.ScriptRefName != nil {
			scriptVolumeMount := corev1.VolumeMount{
				Name:      "script-volume",
				MountPath: "/opt/bin/entrypoint.sh",
				SubPath:   "entrypoint.sh",
				ReadOnly:  true,
			}

			container.VolumeMounts = append(container.VolumeMounts, scriptVolumeMount)
			container.Command = []string{"/bin/bash", "/opt/bin/entrypoint.sh"}
		}
		return container
	}

	sbatchScriptVolumeMount := corev1.VolumeMount{
		Name:      "sbatch-volume",
		MountPath: "/opt/bin/sbatch.sh",
		SubPath:   "sbatch.sh",
		ReadOnly:  true,
	}

	slurmVolumeMounts := []corev1.VolumeMount{
		common.RenderVolumeMountSlurmConfigs(),
		common.RenderVolumeMountMungeKey(),
		common.RenderVolumeMountMungeSocket(),
		sbatchScriptVolumeMount,
	}

	slurmVolumeMounts = append(slurmVolumeMounts, check.Spec.SlurmJobSpec.JobContainer.VolumeMounts...)

	slurmEnvVars := check.Spec.SlurmJobSpec.JobContainer.Env
	slurmEnvVars = append(slurmEnvVars, corev1.EnvVar{
		Name:  consts.ActiveCheckNameEnv,
		Value: check.Name,
	})
	if check.Spec.SlurmJobSpec.EachWorkerJobArray {
		slurmEnvVars = append(slurmEnvVars, corev1.EnvVar{
			Name:  consts.ActiveCheckEachWorkerJobArrayEnv,
			Value: "true",
		})
	} else if check.Spec.SlurmJobSpec.EachWorkerJobs {
		slurmEnvVars = append(slurmEnvVars, corev1.EnvVar{
			Name:  consts.ActiveCheckEachWorkerJobsEnv,
			Value: "true",
		})
	}

	container = corev1.Container{
		Name:            check.Spec.Name,
		Image:           check.Spec.SlurmJobSpec.JobContainer.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             slurmEnvVars,
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{consts.ContainerSecurityContextCapabilitySysAdmin},
			},
		},
		VolumeMounts: slurmVolumeMounts,
	}

	return container
}
