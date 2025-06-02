package prometheus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/render/common"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	slurmprometheus "nebius.ai/slurm-operator/internal/render/prometheus"
	"nebius.ai/slurm-operator/internal/values"
)

var volumeSources = []slurmv1.VolumeSource{
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

func Test_BasePodTemplateSpec(t *testing.T) {
	clusterName := "test-cluster"
	imageExporter := "test-image:latest"
	imageMunge := "test-muge:latest"
	memory := "512Mi"
	cpu := "100m"
	var port int32 = 8080

	initContainers := []corev1.Container{
		common.RenderContainerMunge(&values.Container{
			Name: consts.ContainerNameMunge,
			NodeContainer: slurmv1.NodeContainer{
				Image: imageMunge,
				Resources: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse(memory),
					corev1.ResourceCPU:    resource.MustParse(cpu),
				},
				Port: port,
			},
		}),
	}

	podParams := &values.SlurmExporter{
		Enabled: true,
		ExporterContainer: slurmv1.ExporterContainer{
			NodeContainer: slurmv1.NodeContainer{
				Image: imageExporter,
				Resources: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse(memory),
					corev1.ResourceCPU:    resource.MustParse(cpu),
				},
			},
		},
		SlurmNode: slurmv1.SlurmNode{
			K8sNodeFilterName: "test-node-filter",
		},
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To("test-volume-source"),
		},
	}

	matchLabels := map[string]string{
		"key": "value",
	}

	expected := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: matchLabels,
		},
		Spec: corev1.PodSpec{
			NodeSelector: defaultNodeFilter[0].NodeSelector,
			Affinity:     defaultNodeFilter[0].Affinity,
			InitContainers: []corev1.Container{
				{
					Name:  consts.ContainerNameMunge,
					Image: imageMunge,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse(memory),
							corev1.ResourceCPU:    resource.MustParse(cpu),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse(memory),
						},
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: port,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  consts.ContainerNameExporter,
					Image: imageExporter,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse(memory),
							corev1.ResourceCPU:    resource.MustParse(cpu),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse(memory),
						},
					},
				},
			},
		},
	}

	result := slurmprometheus.BasePodTemplateSpec(clusterName, initContainers, podParams, defaultNodeFilter, volumeSources, matchLabels)

	assert.Equal(t, expected.Labels, result.Labels)

	// expected.Spec.InitContainers[0].Name == munge
	// expected.Spec.Containers[0].Name == exporter

	assert.Equal(t, expected.Spec.InitContainers[0].Name, result.Spec.InitContainers[0].Name)
	assert.Equal(t, expected.Spec.Containers[0].Name, result.Spec.Containers[0].Name)

	assert.Equal(t, expected.Spec.InitContainers[0].Image, result.Spec.InitContainers[0].Image)
	assert.Equal(t, expected.Spec.Containers[0].Image, result.Spec.Containers[0].Image)

	assert.Equal(t, expected.Spec.InitContainers[0].Resources, result.Spec.InitContainers[0].Resources)
	assert.Equal(t, expected.Spec.Containers[0].Resources, result.Spec.Containers[0].Resources)

	assert.Equal(t, expected.Spec.NodeSelector, result.Spec.NodeSelector)
	assert.Equal(t, expected.Spec.Affinity, result.Spec.Affinity)
}

func Test_RenderPodTemplateSpec(t *testing.T) {
	clusterName := "test-cluster"
	imageExporter := "test-image:latest"
	newImageExporter := "new-test-image:latest"
	imageMunge := "test-muge:latest"
	var port int32 = 8080

	initContainers := []corev1.Container{
		common.RenderContainerMunge(&values.Container{
			Name: consts.ContainerNameMunge,
			NodeContainer: slurmv1.NodeContainer{
				Image: imageMunge,
				Port:  port,
			},
		}),
	}

	podParams := &values.SlurmExporter{
		Enabled: true,
		ExporterContainer: slurmv1.ExporterContainer{
			NodeContainer: slurmv1.NodeContainer{
				Image: imageExporter,
			},
		},
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To("test-volume-source"),
		},
	}

	matchLabels := map[string]string{
		"key": "value",
	}

	podTemplateSpec := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			HostNetwork: true,
			OS:          &corev1.PodOS{Name: corev1.Windows},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser:  &[]int64{1000}[0],
				RunAsGroup: &[]int64{1000}[0],
			},
			Containers: []corev1.Container{
				{
					Name:  consts.ContainerNameExporter,
					Image: newImageExporter,
					Args:  []string{"--web.listen-address=:8080"},
				},
			},
		},
	}

	expected := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: matchLabels,
		},
		Spec: corev1.PodSpec{
			NodeSelector: defaultNodeFilter[0].NodeSelector,
			Affinity:     defaultNodeFilter[0].Affinity,
			OS:           &corev1.PodOS{Name: corev1.Windows},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser:  &[]int64{1000}[0],
				RunAsGroup: &[]int64{1000}[0],
			},
			InitContainers: []corev1.Container{
				{
					Name:  consts.ContainerNameMunge,
					Image: imageMunge,
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: port,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  consts.ContainerNameExporter,
					Image: newImageExporter,
					Args:  []string{"--web.listen-address=:8080"},
				},
			},
		},
	}

	result := slurmprometheus.RenderPodTemplateSpec(clusterName, initContainers, podParams, defaultNodeFilter, volumeSources, matchLabels, podTemplateSpec)

	assert.Equal(t, expected.Labels, result.Labels)

	// expected.Spec.InitContainers[0].Name == munge
	// expected.Spec.Containers[0].Name == exporter

	assert.Equal(t, expected.Spec.InitContainers[0].Name, result.Spec.InitContainers[0].Name)
	assert.Equal(t, expected.Spec.Containers[0].Name, result.Spec.Containers[0].Name)

	assert.Equal(t, expected.Spec.InitContainers[0].Image, result.Spec.InitContainers[0].Image)
	assert.Equal(t, expected.Spec.Containers[0].Image, result.Spec.Containers[0].Image)

	assert.Equal(t, expected.Spec.OS, result.Spec.OS)
	assert.Equal(t, expected.Spec.SecurityContext, result.Spec.SecurityContext)

	assert.Equal(t, expected.Spec.Containers[0].Args, result.Spec.Containers[0].Args)
}
