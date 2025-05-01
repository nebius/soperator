package soperatorchecks

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func renderPodTemplateSpec(check *slurmv1alpha1.ActiveCheck, labels map[string]string) corev1.PodTemplateSpec {
	var initContainers []corev1.Container
	var annotations map[string]string

	if check.Spec.CheckType == "slurmJob" {
		mungeContainerValues := values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image:   check.Spec.SlurmJobSpec.MungeContainer.Image,
				Command: check.Spec.SlurmJobSpec.MungeContainer.Command,
			},
			Name: "munge",
		}

		mungeContainer := common.RenderContainerMunge(&mungeContainerValues)
		initContainers = append(initContainers, mungeContainer)

		annotations = map[string]string{
			fmt.Sprintf(
				"%s/%s", consts.AnnotationApparmorKey, check.Spec.Name,
			): check.Spec.SlurmJobSpec.JobContainer.AppArmorProfile,
			fmt.Sprintf(
				"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameMunge,
			): check.Spec.SlurmJobSpec.MungeContainer.AppArmorProfile,
			consts.DefaultContainerAnnotationName: consts.ContainerNameAccounting,
		}
	}

	if check.Spec.CheckType == "k8sJob" {
		annotations = map[string]string{
			fmt.Sprintf(
				"%s/%s", consts.AnnotationApparmorKey, check.Spec.Name,
			): check.Spec.K8sJobSpec.JobContainer.AppArmorProfile,
		}
	}

	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			Affinity:              check.Spec.Affinity,
			NodeSelector:          check.Spec.NodeSelector,
			Tolerations:           check.Spec.Tolerations,
			ActiveDeadlineSeconds: ptr.To(check.Spec.ActiveDeadlineSeconds),
			RestartPolicy:         corev1.RestartPolicyNever,
			Volumes:               renderVolumes(check),
			Containers:            []corev1.Container{renderContainerK8sCronjob(check)},
			InitContainers:        initContainers,
		},
	}
}

func renderVolumes(check *slurmv1alpha1.ActiveCheck) []corev1.Volume {
	var volumes []corev1.Volume

	slurmVolumes := []corev1.Volume{
		common.RenderVolumeSlurmConfigs(check.Spec.SlurmClusterRefName),
		common.RenderVolumeMungeKey(check.Spec.SlurmClusterRefName),
		common.RenderVolumeMungeSocket(),
	}

	switch check.Spec.CheckType {
	case "k8sJob":
		volumes = check.Spec.K8sJobSpec.JobContainer.Volumes
	case "slurmJob":
		volumes = check.Spec.SlurmJobSpec.JobContainer.Volumes
		volumes = append(volumes, slurmVolumes...)
	}

	if check.Spec.CheckType == "k8sJob" && check.Spec.K8sJobSpec.ScriptRefName != nil {
		scriptVolume := corev1.Volume{
			Name: "script-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: *check.Spec.K8sJobSpec.ScriptRefName,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  "script.sh",
							Path: "entrypoint.sh",
							Mode: ptr.To(int32(0755)),
						},
					},
				},
			},
		}
		volumes = append(volumes, scriptVolume)
	}

	if check.Spec.CheckType == "slurmJob" {
		var sbatchScriptName string
		if check.Spec.SlurmJobSpec.SbatchScriptRefName != nil {
			sbatchScriptName = *check.Spec.SlurmJobSpec.SbatchScriptRefName

		} else {
			sbatchScriptName = naming.BuildConfigMapSbatchScriptName(check.Spec.Name)
		}
		if check.Spec.SlurmJobSpec.SbatchScriptRefName == nil {
			sbatchScriptName = naming.BuildConfigMapSbatchScriptName(check.Spec.Name)
		}

		scriptVolume := corev1.Volume{
			Name: "sbatch-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: sbatchScriptName,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  consts.ConfigMapKeySoperatorcheckSbatch,
							Path: consts.ConfigMapKeySoperatorcheckSbatch,
							Mode: ptr.To(int32(0755)),
						},
					},
				},
			},
		}
		volumes = append(volumes, scriptVolume)
	}

	return volumes
}
