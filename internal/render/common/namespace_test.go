package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

type namespaceTestConfig struct {
	Defaults namespaceTestOptions `yaml:"defaults"`
	Nodes    []struct {
		Names   []string             `yaml:"nodes"`
		Options namespaceTestOptions `yaml:"options"`
	} `yaml:"node_confs"`
}

type namespaceTestOptions struct {
	AutoBasePath bool                     `yaml:"auto_base_path"`
	BasePath     string                   `yaml:"base_path"`
	Shared       bool                     `yaml:"shared"`
	Directories  []namespaceTestDirectory `yaml:"dir_confs"`
}

type namespaceTestDirectory struct {
	Path    string `yaml:"path"`
	Tmpfs   bool   `yaml:"tmpfs"`
	Options string `yaml:"options"`
}

func parseNamespaceConfig(t *testing.T, rendered string) namespaceTestConfig {
	t.Helper()
	var config namespaceTestConfig
	decoder := yaml.NewDecoder(strings.NewReader(rendered))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&config))
	return config
}

func TestGenerateNamespaceConfig(t *testing.T) {
	cluster := &values.SlurmCluster{
		NodeSets: []slurmv1alpha1.NodeSet{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "gpu-workers"},
				Spec: slurmv1alpha1.NodeSetSpec{
					Replicas: 2,
					Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
						Resources: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Gi")},
						Volumes: slurmv1alpha1.WorkerVolumesSpec{
							SharedMemorySize: ptr.To(resource.MustParse("16Gi")),
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "disabled"},
				Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 0},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "cpu-worker"},
				Spec: slurmv1alpha1.NodeSetSpec{
					Replicas: 1,
					Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
						Resources: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("96Gi")},
						Volumes: slurmv1alpha1.WorkerVolumesSpec{
							SharedMemorySize: ptr.To(resource.MustParse("64Gi")),
						},
					},
				},
			},
		},
	}
	config := parseNamespaceConfig(t, generateNamespaceConfig(cluster).Render())
	assert.True(t, config.Defaults.AutoBasePath)
	assert.Equal(t, "/var/spool/slurmd/job-container/%n", config.Defaults.BasePath)
	assert.True(t, config.Defaults.Shared)
	assert.Equal(t, []namespaceTestDirectory{
		{Path: "/mnt/memory", Tmpfs: true, Options: "mode=1777"},
		{Path: "/dev/shm", Tmpfs: true, Options: "mode=1777"},
		{Path: "/tmp", Tmpfs: true, Options: "mode=1777"},
	}, config.Defaults.Directories)
	require.Len(t, config.Nodes, 2)
	assert.Equal(t, []string{"gpu-workers-[0-1]"}, config.Nodes[0].Names)
	assert.Equal(t, []namespaceTestDirectory{
		{Path: "/mnt/memory", Tmpfs: true, Options: "mode=1777,size=137438953472"},
		{Path: "/dev/shm", Tmpfs: true, Options: "mode=1777,size=17179869184"},
		{Path: "/tmp", Tmpfs: true, Options: "mode=1777,size=137438953472"},
	}, config.Nodes[0].Options.Directories)
	assert.Equal(t, []string{"cpu-worker-0"}, config.Nodes[1].Names)
	assert.Equal(t, []namespaceTestDirectory{
		{Path: "/mnt/memory", Tmpfs: true, Options: "mode=1777,size=103079215104"},
		{Path: "/dev/shm", Tmpfs: true, Options: "mode=1777,size=68719476736"},
		{Path: "/tmp", Tmpfs: true, Options: "mode=1777,size=103079215104"},
	}, config.Nodes[1].Options.Directories)
}

func TestGenerateNamespaceConfigWithoutLimits(t *testing.T) {
	cluster := &values.SlurmCluster{
		NodeSets: []slurmv1alpha1.NodeSet{{
			ObjectMeta: metav1.ObjectMeta{Name: "workers"},
			Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 1},
		}},
	}
	config := parseNamespaceConfig(t, generateNamespaceConfig(cluster).Render())
	require.Len(t, config.Nodes, 1)
	assert.Equal(t, config.Defaults.Directories, config.Nodes[0].Options.Directories)
	config = parseNamespaceConfig(t, generateNamespaceConfig(&values.SlurmCluster{}).Render())
	assert.Empty(t, config.Nodes)
}

func TestNamespaceConfigAtTenThousandNodes(t *testing.T) {
	cluster := &values.SlurmCluster{
		NodeSets: []slurmv1alpha1.NodeSet{{
			ObjectMeta: metav1.ObjectMeta{Name: "workers"},
			Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 10000},
		}},
	}
	rendered := generateNamespaceConfig(cluster).Render()
	config := parseNamespaceConfig(t, rendered)
	require.Len(t, config.Nodes, 1)
	assert.Equal(t, []string{"workers-[0-9999]"}, config.Nodes[0].Names)
	assert.Less(t, len(rendered), 1024)
}

func TestNamespaceConfigDistribution(t *testing.T) {
	cluster := &values.SlurmCluster{}
	configMap := RenderConfigMapSlurmConfigs(cluster)
	jailedConfig := RenderJailedConfigSlurmConfigs(cluster)
	assert.Contains(t, configMap.Data[consts.ConfigMapKeySlurmBaseConfig], "NamespaceType=namespace/linux")
	assert.Contains(t, configMap.Data[consts.ConfigMapKeySlurmBaseConfig], "PrologFlags=contain")
	assert.Equal(t, configMap.Name, jailedConfig.Spec.ConfigMap.Name)
	assert.Equal(t, configMap.Name, jailedConfig.Name)
	assert.Contains(t, jailedConfig.Spec.Items, corev1.KeyToPath{
		Key: consts.ConfigMapKeyNamespaceConfig, Path: "/etc/slurm/namespace.yaml",
	})
	require.Contains(t, configMap.Data, consts.ConfigMapKeyNamespaceConfig)
	config := parseNamespaceConfig(t, configMap.Data[consts.ConfigMapKeyNamespaceConfig])
	require.Len(t, config.Defaults.Directories, 3)
	assert.Contains(t, jailedConfig.Spec.UpdateActions, slurmv1alpha1.UpdateActionReconfigure)
}
