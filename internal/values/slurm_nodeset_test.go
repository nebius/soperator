package values

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

func TestBuildSlurmNodeSetFrom_SSSD(t *testing.T) {
	makeNodeSet := func() *slurmv1alpha1.NodeSet {
		return &slurmv1alpha1.NodeSet{
			ObjectMeta: metav1ObjectMeta("worker-a", "test-ns"),
			Spec: slurmv1alpha1.NodeSetSpec{
				Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
					Image: slurmv1alpha1.Image{Repository: "slurmd", Tag: "latest"},
					Volumes: slurmv1alpha1.WorkerVolumesSpec{
						Spool: corev1.VolumeSource{},
						Jail:  corev1.VolumeSource{},
					},
				},
				Munge: slurmv1alpha1.ContainerMungeSpec{
					Image: slurmv1alpha1.Image{Repository: "munge", Tag: "latest"},
				},
			},
		}
	}

	t.Run("uses default nodeset secret name when ref is empty", func(t *testing.T) {
		nodeSet := makeNodeSet()
		nodeSet.Spec.SSSD = &slurmv1alpha1.ContainerSSSDSpec{
			Image: slurmv1alpha1.Image{Repository: "sssd", Tag: "latest"},
		}

		result := BuildSlurmNodeSetFrom(nodeSet, "test-cluster", nil, false)

		if assert.NotNil(t, result.ContainerSSSD) {
			assert.Equal(t, "sssd:latest", result.ContainerSSSD.Image)
			assert.Equal(t, consts.ContainerNameSSSD, result.ContainerSSSD.Name)
		}
		assert.True(t, result.IsSSSDSecretDefault)
		assert.Equal(t, naming.BuildNodeSetSecretSSSDConfName("test-cluster", "worker-a"), result.SSSDConfSecretName)
	})

	t.Run("uses referenced secret name when ref is set", func(t *testing.T) {
		nodeSet := makeNodeSet()
		nodeSet.Spec.SSSD = &slurmv1alpha1.ContainerSSSDSpec{
			Image: slurmv1alpha1.Image{Repository: "sssd", Tag: "latest"},
		}
		nodeSet.Spec.SSSDConfSecretRefName = "custom-worker-sssd"

		result := BuildSlurmNodeSetFrom(nodeSet, "test-cluster", nil, false)

		assert.False(t, result.IsSSSDSecretDefault)
		assert.Equal(t, "custom-worker-sssd", result.SSSDConfSecretName)
	})

	t.Run("keeps sssd disabled when container is not configured", func(t *testing.T) {
		nodeSet := makeNodeSet()

		result := BuildSlurmNodeSetFrom(nodeSet, "test-cluster", nil, false)

		assert.Nil(t, result.ContainerSSSD)
		assert.Empty(t, result.SSSDConfSecretName)
		assert.False(t, result.IsSSSDSecretDefault)
	})
}

func metav1ObjectMeta(name, namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
}
