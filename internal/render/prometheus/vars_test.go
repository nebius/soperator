package prometheus_test

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

var (
	defaultNamespace       = "test-namespace"
	defaultNameCluster     = "test-cluster"
	defaultPodTemplateName = "test-pod-template"
	imageExporter          = "image-exporter:latest"
	defaultSlurmAPIServer  = "http://slurm-api-server"

	defaultPodTemplate = &corev1.PodTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultPodTemplateName,
			Namespace: defaultNamespace,
		},
		Template: *defaultPodTemplateSpec,
	}
	defaultPodTemplateSpec = &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser:  &[]int64{1000}[0],
				RunAsGroup: &[]int64{1000}[0],
			},
			Containers: []corev1.Container{
				{
					Name:  consts.ContainerNameExporter,
					Image: imageExporter,
					Args:  []string{"--web.listen-address=:8080"},
				},
			},
		},
	}
	defaultVolumeSources = []slurmv1.VolumeSource{
		{
			Name: "test-volume-source",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "jail-pvc",
					ReadOnly:  true,
				},
			},
		},
	}

	defaultNodeFilter = []slurmv1.K8sNodeFilter{
		{
			Name: "test-node-filter",
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "key",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"value"},
									},
								},
							},
						},
					},
				},
			},
			NodeSelector: map[string]string{
				"key": "value",
			},
			Tolerations: []corev1.Toleration{
				{
					Key:      "key",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		},
	}
)
