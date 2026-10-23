package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/values"
)

func TestGenerateNamespaceConfig(t *testing.T) {
	var config struct {
		Defaults struct {
			AutoBasePath bool   `yaml:"auto_base_path"`
			BasePath     string `yaml:"base_path"`
			Shared       bool   `yaml:"shared"`
			Directories  []struct {
				Path    string `yaml:"path"`
				Tmpfs   bool   `yaml:"tmpfs"`
				Options string `yaml:"options"`
			} `yaml:"dir_confs"`
		} `yaml:"defaults"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(generateNamespaceConfig().Render()))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&config))
	assert.False(t, config.Defaults.AutoBasePath)
	assert.Equal(t, "/tmp", config.Defaults.BasePath)
	assert.True(t, config.Defaults.Shared)
	require.Len(t, config.Defaults.Directories, 3)
	for i, path := range []string{"/mnt/jail/dev/shm", "/mnt/jail/tmp", "/mnt/jail/mnt/memory"} {
		assert.Equal(t, path, config.Defaults.Directories[i].Path)
		assert.True(t, config.Defaults.Directories[i].Tmpfs)
		assert.Equal(t, "mode=1777", config.Defaults.Directories[i].Options)
	}
}

func TestNamespaceConfigAtTenThousandNodes(t *testing.T) {
	cluster := &values.SlurmCluster{
		NamespaceConfig: values.NamespaceConfig{Enabled: true},
		NodeSets: []slurmv1alpha1.NodeSet{{
			Spec: slurmv1alpha1.NodeSetSpec{Replicas: 10000},
		}},
	}
	config := RenderConfigMapSlurmConfigs(cluster).Data[consts.ConfigMapKeyNamespaceConfig]
	assert.Equal(t, WithManagedSlurmConfigWarning(generateNamespaceConfig()).Render(), config)
	assert.NotContains(t, config, "node_confs")
	assert.Less(t, len(config), 1024)
}

func TestRenderJailedConfigNamespace(t *testing.T) {
	cluster := &values.SlurmCluster{
		NamespacedName:  types.NamespacedName{Name: "test-cluster", Namespace: "slurm"},
		NamespaceConfig: values.NamespaceConfig{Enabled: true},
	}
	generated := RenderJailedConfigNamespace(cluster)
	assert.Equal(t, naming.BuildJailedConfigNamespaceName(cluster.Name), generated.Name)
	assert.Equal(t, cluster.Namespace, generated.Namespace)
	assert.Equal(t, naming.BuildConfigMapSlurmConfigsName(cluster.Name), generated.Spec.ConfigMap.Name)
	assert.Equal(t, cluster.Name, generated.Labels[consts.LabelInstanceKey])
	assert.Equal(t, consts.LabelJailedAggregationCommonValue, generated.Labels[consts.LabelJailedAggregationKey])
	assert.Equal(t, []corev1.KeyToPath{{Key: consts.ConfigMapKeyNamespaceConfig, Path: "/etc/slurm/namespace.yaml"}}, generated.Spec.Items)
	assert.Equal(t, []slurmv1alpha1.UpdateAction{slurmv1alpha1.UpdateActionReconfigure}, generated.Spec.UpdateActions)

	cluster.NamespaceConfig.ConfigMapName = "custom-namespace"
	custom := RenderJailedConfigNamespace(cluster)
	assert.Equal(t, generated.Name, custom.Name)
	assert.Equal(t, generated.Namespace, custom.Namespace)
	assert.Equal(t, "custom-namespace", custom.Spec.ConfigMap.Name)
	assert.Equal(t, generated.Spec.Items, custom.Spec.Items)
}
