package soperatorchecks

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func renderPodTemplateSpec(check *slurmv1alpha1.ActiveCheck, labels map[string]string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			Affinity:              check.Spec.Affinity,
			NodeSelector:          check.Spec.NodeSelector,
			Tolerations:           check.Spec.Tolerations,
			ActiveDeadlineSeconds: ptr.To(check.Spec.ActiveDeadlineSeconds),
			RestartPolicy:         corev1.RestartPolicyNever,
			Volumes:               check.Spec.K8sJobSpec.Volumes,
			Containers:            []corev1.Container{renderContainerK8sCronjob(check)},
		},
	}
}
