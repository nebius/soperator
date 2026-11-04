package nodeconfigurator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

func fieldRefFor(t *testing.T, container corev1.Container, name string) string {
	t.Helper()

	for _, env := range container.Env {
		if env.Name != name {
			continue
		}
		require.NotNil(t, env.ValueFrom, "env %s has no valueFrom", name)
		require.NotNil(t, env.ValueFrom.FieldRef, "env %s has no fieldRef", name)
		return env.ValueFrom.FieldRef.FieldPath
	}

	t.Fatalf("env %s is not rendered", name)
	return ""
}

// The rebooter learns its own node from the downward API, so it never has to look one up
// against the API server. The kubelet endpoint comes from that node's status instead.
func TestRenderContainerRebooterNodeNameFromDownwardAPI(t *testing.T) {
	container := renderContainerRebooter(slurmv1alpha1.Rebooter{Enabled: true})

	assert.Equal(t, "spec.nodeName", fieldRefFor(t, container, consts.RebooterNodeNameEnv))
}

func TestRenderContainerRebooterKeepsUserEnv(t *testing.T) {
	container := renderContainerRebooter(slurmv1alpha1.Rebooter{
		Enabled: true,
		ContainerConfig: slurmv1alpha1.ContainerConfig{
			Env: []corev1.EnvVar{{Name: "CUSTOM", Value: "value"}},
		},
	})

	assert.Contains(t, container.Env, corev1.EnvVar{Name: "CUSTOM", Value: "value"})
}
