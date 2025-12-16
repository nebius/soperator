package soperatorchecks

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
)

func renderContainerK8sCronjob(check *slurmv1alpha1.ActiveCheck) corev1.Container {
	var container corev1.Container

	if check.Spec.CheckType == "k8sJob" {
		volumeMounts := check.Spec.K8sJobSpec.JobContainer.VolumeMounts

		// Add slurm volume mounts if munge is enabled
		if check.Spec.K8sJobSpec.MungeContainer != nil {
			volumeMounts = append(volumeMounts,
				common.RenderVolumeMountSlurmConfigs(),
				common.RenderVolumeMountMungeSocket(),
			)
		}

		container = corev1.Container{
			Name:            check.Spec.Name,
			Image:           check.Spec.K8sJobSpec.JobContainer.Image,
			Command:         check.Spec.K8sJobSpec.JobContainer.Command,
			Args:            check.Spec.K8sJobSpec.JobContainer.Args,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Env:             check.Spec.K8sJobSpec.JobContainer.Env,
			SecurityContext: &corev1.SecurityContext{
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{consts.ContainerSecurityContextCapabilitySysAdmin},
				},
				AppArmorProfile: common.ParseAppArmorProfile(
					check.Spec.K8sJobSpec.JobContainer.AppArmorProfile),
			},
			VolumeMounts: volumeMounts,
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
	if check.Spec.SlurmJobSpec.EachWorkerJobs {
		slurmEnvVars = append(slurmEnvVars, corev1.EnvVar{
			Name:  consts.ActiveCheckEachWorkerJobsEnv,
			Value: "true",
		})
	}
	if check.Spec.SlurmJobSpec.MaxNumberOfJobs != nil {
		slurmEnvVars = append(slurmEnvVars, corev1.EnvVar{
			Name:  consts.ActiveCheckMaxNumberOfJobsEnv,
			Value: fmt.Sprint(*check.Spec.SlurmJobSpec.MaxNumberOfJobs),
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
			AppArmorProfile: common.ParseAppArmorProfile(
				check.Spec.K8sJobSpec.JobContainer.AppArmorProfile),
		},
		VolumeMounts: slurmVolumeMounts,
	}

	return container
}
