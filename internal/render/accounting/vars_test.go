package accounting_test

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

var (
	defaultNamespace   = "test-namespace"
	defaultNameCluster = "test-cluster"
	image              = "image-acc:latest"
	imageMunge         = "test-muge:latest"
	memory             = "512Mi"
	cpu                = "100m"

	port int32 = 8080

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
	munge = &values.Container{
		Name: consts.ContainerNameMunge,
		NodeContainer: slurmv1.NodeContainer{
			Image: imageMunge,
			Resources: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memory),
				corev1.ResourceCPU:    resource.MustParse(cpu),
			},
			Port: port,
		},
	}

	user        = "test-user"
	secretName  = "test-secret"
	passwordKey = "password"

	acc = &values.SlurmAccounting{
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To("test-volume-source"),
		},
		SlurmNode: slurmv1.SlurmNode{
			K8sNodeFilterName: "test-node-filter",
		},
		ContainerAccounting: values.Container{
			Name: consts.ContainerNameAccounting,
			NodeContainer: slurmv1.NodeContainer{
				Image: image,
				Resources: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse(memory),
					corev1.ResourceCPU:    resource.MustParse(cpu),
				},
			},
		},
		ContainerMunge: *munge,
		Service: values.Service{
			Name: "test-service",
		},
		Deployment: values.Deployment{
			Name: "test-deployment",
		},
		ExternalDB: slurmv1.ExternalDB{
			Host: "test-host",
			Port: 5432,
			User: user,
			Secret: slurmv1.SecretAccounting{
				Name:        secretName,
				PasswordKey: passwordKey,
			},
		},
		Enabled: true,
	}

	matchLabels = map[string]string{
		"key": "value",
	}

	defaultSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: defaultNamespace,
		},
		Data: map[string][]byte{
			passwordKey: []byte("test-password"),
		},
	}
)
