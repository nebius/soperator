package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_GenerateCGroupConfig(t *testing.T) {
	// Test with CgroupVersion set to CGroupV2
	clusterV2 := &values.SlurmCluster{
		NodeWorker: values.SlurmWorker{
			CgroupVersion: consts.CGroupV2,
		},
	}
	resV2 := generateCGroupConfig(clusterV2)
	expectedV2 := `CgroupMountpoint=/sys/fs/cgroup
ConstrainCores=yes
ConstrainDevices=yes
ConstrainRAMSpace=yes
CgroupPlugin=cgroup/v2
ConstrainSwapSpace=no
EnableControllers=yes
IgnoreSystemd=yes`
	assert.Equal(t, expectedV2, resV2.Render())
	clusterV1 := &values.SlurmCluster{
		NodeWorker: values.SlurmWorker{
			CgroupVersion: consts.CGroupV1,
		},
	}
	expectedV1 := `CgroupMountpoint=/sys/fs/cgroup
ConstrainCores=yes
ConstrainDevices=yes
ConstrainRAMSpace=yes
CgroupPlugin=cgroup/v1
ConstrainSwapSpace=yes`
	resV1 := generateCGroupConfig(clusterV1)
	assert.Equal(t, expectedV1, resV1.Render())

}

func TestRenderConfigMapSecurityLimits(t *testing.T) {
	tests := []struct {
		name          string
		cluster       values.SlurmCluster
		componentType consts.ComponentType
		expectedData  string
		expectedLabel string
	}{
		{
			name: "Login default security limits",
			cluster: values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "slurm",
					Name:      "test",
				},
			},
			componentType: consts.ComponentTypeLogin,
			expectedData:  "#Empty security limits file",
			expectedLabel: consts.ComponentTypeLogin.String(),
		},
		{
			name: "Login custom security limits",
			cluster: values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "slurm",
					Name:      "test",
				},
				NodeLogin: values.SlurmLogin{
					ContainerSshd: values.Container{
						NodeContainer: slurmv1.NodeContainer{
							SecurityLimitsConfig: "* soft memlock 500000\n* hard memlock 500000",
						},
					},
				},
			},
			componentType: consts.ComponentTypeLogin,
			expectedData:  "* soft memlock 500000\n* hard memlock 500000",
			expectedLabel: consts.ComponentTypeLogin.String(),
		},
		{
			name: "Worker default security limits",
			cluster: values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "slurm",
					Name:      "test",
				},
			},
			componentType: consts.ComponentTypeWorker,
			expectedData:  generateUnlimitedSecurityLimitsConfig().Render(),
			expectedLabel: consts.ComponentTypeWorker.String(),
		},
		{
			name: "Worker custom security limits",
			cluster: values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "slurm",
					Name:      "test",
				},
				NodeWorker: values.SlurmWorker{
					ContainerSlurmd: values.Container{
						NodeContainer: slurmv1.NodeContainer{
							SecurityLimitsConfig: "* soft memlock 300000\n* hard memlock 300000",
						},
					},
				},
			},
			componentType: consts.ComponentTypeWorker,
			expectedData:  "* soft memlock 300000\n* hard memlock 300000",
			expectedLabel: consts.ComponentTypeWorker.String(),
		},
		{
			name: "Controller default security limits",
			cluster: values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "slurm",
					Name:      "test",
				},
			},
			componentType: consts.ComponentTypeController,
			expectedData:  "#Empty security limits file",
			expectedLabel: "controller",
		},
		{
			name: "Controller custom security limits",
			cluster: values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "slurm",
					Name:      "test",
				},
				NodeController: values.SlurmController{
					ContainerSlurmctld: values.Container{
						NodeContainer: slurmv1.NodeContainer{
							SecurityLimitsConfig: "* soft memlock 100000\n* hard memlock 100000",
						},
					},
				},
			},
			componentType: consts.ComponentTypeController,
			expectedData:  "* soft memlock 100000\n* hard memlock 100000",
			expectedLabel: consts.ComponentTypeController.String(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			result := RenderConfigMapSecurityLimits(tt.componentType, &tt.cluster)

			assert.NotNil(t, result)
			assert.Equal(t, tt.expectedLabel, result.Labels[consts.LabelComponentKey])
			assert.Equal(t, tt.expectedData, result.Data[consts.ConfigMapKeySecurityLimits])
		})
	}
}

func TestRenderSlurmConfigMapAndTopology(t *testing.T) {
	tests := []struct {
		name                     string
		cluster                  values.SlurmCluster
		expectedTopologyPlugin   string
		unexpectedTopologyPlugin string
	}{
		{
			name: "No topology config",
			cluster: values.SlurmCluster{
				SlurmConfig: slurmv1.SlurmConfig{
					TopologyPlugin: "",
				},
			},
			expectedTopologyPlugin:   "",
			unexpectedTopologyPlugin: "",
		},
		{
			name: "Override topology plugin",
			cluster: values.SlurmCluster{
				SlurmConfig: slurmv1.SlurmConfig{
					TopologyPlugin: "topology/block",
				},
			},
			expectedTopologyPlugin:   "topology/block",
			unexpectedTopologyPlugin: "topology/tree",
		},
		{
			name: "ConfigMap exists but topology config inside",
			cluster: values.SlurmCluster{
				SlurmConfig: slurmv1.SlurmConfig{
					TopologyPlugin: "",
				},
			},
			expectedTopologyPlugin:   "",
			unexpectedTopologyPlugin: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			result := RenderConfigMapSlurmConfigs(&tt.cluster)
			assert.NotNil(t, result)

			if tt.expectedTopologyPlugin == "" {
				assert.NotContains(t, result.Data[consts.ConfigMapKeySlurmConfig], "TopologyPlugin")
			} else {
				assert.Contains(t, result.Data[consts.ConfigMapKeySlurmConfig], "TopologyPlugin="+tt.expectedTopologyPlugin)
			}

			if tt.unexpectedTopologyPlugin != "" {
				assert.NotContains(t, result.Data[consts.ConfigMapKeySlurmConfig], "TopologyPlugin="+tt.unexpectedTopologyPlugin)
			}
		})
	}
}

func TestRenderPlugstack(t *testing.T) {
	t.Run("Pyxis no options", func(t *testing.T) {
		result := generateSpankConfig(&values.SlurmCluster{
			PlugStackConfig: slurmv1.PlugStackConfig{
				Pyxis: slurmv1.PluginConfigPyxis{},
			},
		}).Render()
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "optional spank_pyxis.so runtime_path=/run/pyxis execute_entrypoint=0 container_scope=global sbatch_support=1 container_image_save=")
	})

	t.Run("Pyxis options", func(t *testing.T) {
		result := generateSpankConfig(&values.SlurmCluster{
			PlugStackConfig: slurmv1.PlugStackConfig{
				Pyxis: slurmv1.PluginConfigPyxis{
					Required:           ptr.To(true),
					ContainerImageSave: "/tmp/",
				},
			},
		}).Render()
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "required spank_pyxis.so runtime_path=/run/pyxis execute_entrypoint=0 container_scope=global sbatch_support=1 container_image_save=/tmp/")
	})

	t.Run("NCCL no options", func(t *testing.T) {
		result := generateSpankConfig(&values.SlurmCluster{
			PlugStackConfig: slurmv1.PlugStackConfig{
				NcclDebug: slurmv1.PluginConfigNcclDebug{},
			},
		}).Render()
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "optional spanknccldebug.so enabled=0 log-level=INFO out-file=0 out-dir=/opt/soperator-outputs/nccl_logs out-stdout=0")
	})

	t.Run("NCCL options", func(t *testing.T) {
		result := generateSpankConfig(&values.SlurmCluster{
			PlugStackConfig: slurmv1.PlugStackConfig{
				NcclDebug: slurmv1.PluginConfigNcclDebug{
					Required:        true,
					Enabled:         ptr.To(true),
					LogLevel:        "TRACE",
					OutputToFile:    true,
					OutputDirectory: "/tmp",
					OutputToStdOut:  true,
				},
			},
		}).Render()
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "required spanknccldebug.so enabled=1 log-level=TRACE out-file=1 out-dir=/tmp out-stdout=1")
	})

	t.Run("Custom not provided", func(t *testing.T) {
		result := generateSpankConfig(&values.SlurmCluster{
			PlugStackConfig: slurmv1.PlugStackConfig{
				CustomPlugins: []slurmv1.PluginConfigCustom{},
			},
		}).Render()
		assert.NotEmpty(t, result)
		assert.Equal(t, 3, len(strings.Split(result, "\n")))
	})

	t.Run("Custom no options", func(t *testing.T) {
		result := generateSpankConfig(&values.SlurmCluster{
			PlugStackConfig: slurmv1.PlugStackConfig{
				CustomPlugins: []slurmv1.PluginConfigCustom{{
					Path: "/lol/kek.so",
				}},
			},
		}).Render()
		assert.NotEmpty(t, result)
		assert.Equal(t, 4, len(strings.Split(result, "\n")))
		assert.Contains(t, result, "optional /lol/kek.so")
	})

	t.Run("Custom options", func(t *testing.T) {
		result := generateSpankConfig(&values.SlurmCluster{
			PlugStackConfig: slurmv1.PlugStackConfig{
				CustomPlugins: []slurmv1.PluginConfigCustom{{
					Required: true,
					Path:     "/lol/kek.so",
					Arguments: map[string]string{
						"lol": "kek",
					},
				}, {
					Required: true,
					Path:     "/kek/lol.so",
					Arguments: map[string]string{
						"kek": "lol",
					},
				}},
			},
		}).Render()
		assert.NotEmpty(t, result)
		assert.Equal(t, 5, len(strings.Split(result, "\n")))
		assert.Contains(t, result, "required /lol/kek.so lol=kek")
		assert.Contains(t, result, "required /kek/lol.so kek=lol")
	})
}

func TestBuildNodeSetNodeNamesMap(t *testing.T) {
	tests := []struct {
		name     string
		nodeSets []slurmv1alpha1.NodeSet
		expected map[string]string
	}{
		{
			name:     "Empty NodeSet list",
			nodeSets: []slurmv1alpha1.NodeSet{},
			expected: map[string]string{},
		},
		{
			name: "Single NodeSet with 1 replica",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: slurmv1alpha1.NodeSetSpec{
						Replicas: 1,
					},
				},
			},
			expected: map[string]string{
				"test": "test-0",
			},
		},
		{
			name: "Single NodeSet with 3 replicas",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: slurmv1alpha1.NodeSetSpec{
						Replicas: 3,
					},
				},
			},
			expected: map[string]string{
				"test": "test-0,test-1,test-2",
			},
		},
		{
			name: "Multiple NodeSets with different replicas",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu",
					},
					Spec: slurmv1alpha1.NodeSetSpec{
						Replicas: 2,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cpu",
					},
					Spec: slurmv1alpha1.NodeSetSpec{
						Replicas: 4,
					},
				},
			},
			expected: map[string]string{
				"gpu": "gpu-0,gpu-1",
				"cpu": "cpu-0,cpu-1,cpu-2,cpu-3",
			},
		},
		{
			name: "NodeSet with 0 replicas",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "empty",
					},
					Spec: slurmv1alpha1.NodeSetSpec{
						Replicas: 0,
					},
				},
			},
			expected: map[string]string{
				"empty": "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildNodeSetNodeNamesMap(tt.nodeSets)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAddStructuredPartitions(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *values.SlurmCluster
		expected string
	}{
		{
			name: "Empty partitions list",
			cluster: &values.SlurmCluster{
				PartitionConfiguration: values.PartitionConfiguration{
					Partitions: []slurmv1.Partition{},
				},
				NodeSetList: slurmv1alpha1.NodeSetList{
					Items: []slurmv1alpha1.NodeSet{},
				},
			},
			expected: "## Structured partitions will be generated by partition config reconciler\n## WARNING: No partitions defined in structured configuration!",
		},
		{
			name: "Single partition with one NodeSet",
			cluster: &values.SlurmCluster{
				PartitionConfiguration: values.PartitionConfiguration{
					Partitions: []slurmv1.Partition{
						{
							Name:        "main",
							NodeSetRefs: []string{"gpu"},
							Config:      "Default=YES State=UP",
						},
					},
				},
				NodeSetList: slurmv1alpha1.NodeSetList{
					Items: []slurmv1alpha1.NodeSet{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "gpu"},
							Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 2},
						},
					},
				},
			},
			expected: "## Structured partitions will be generated by partition config reconciler\nPartitionName=main Nodes=gpu-0,gpu-1 Default=YES State=UP",
		},
		{
			name: "Multiple partitions with different NodeSets",
			cluster: &values.SlurmCluster{
				PartitionConfiguration: values.PartitionConfiguration{
					Partitions: []slurmv1.Partition{
						{
							Name:        "gpu-partition",
							NodeSetRefs: []string{"gpu"},
							Config:      "State=UP",
						},
						{
							Name:        "cpu-partition",
							NodeSetRefs: []string{"cpu"},
							Config:      "State=UP",
						},
					},
				},
				NodeSetList: slurmv1alpha1.NodeSetList{
					Items: []slurmv1alpha1.NodeSet{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "gpu"},
							Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 2},
						},
						{
							ObjectMeta: metav1.ObjectMeta{Name: "cpu"},
							Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 3},
						},
					},
				},
			},
			expected: "## Structured partitions will be generated by partition config reconciler\nPartitionName=gpu-partition Nodes=gpu-0,gpu-1 State=UP\nPartitionName=cpu-partition Nodes=cpu-0,cpu-1,cpu-2 State=UP",
		},
		{
			name: "Partition with multiple NodeSetRefs",
			cluster: &values.SlurmCluster{
				PartitionConfiguration: values.PartitionConfiguration{
					Partitions: []slurmv1.Partition{
						{
							Name:        "combined",
							NodeSetRefs: []string{"gpu", "cpu"},
							Config:      "State=UP",
						},
					},
				},
				NodeSetList: slurmv1alpha1.NodeSetList{
					Items: []slurmv1alpha1.NodeSet{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "gpu"},
							Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 1},
						},
						{
							ObjectMeta: metav1.ObjectMeta{Name: "cpu"},
							Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 2},
						},
					},
				},
			},
			expected: "## Structured partitions will be generated by partition config reconciler\nPartitionName=combined Nodes=gpu-0,cpu-0,cpu-1 State=UP",
		},
		{
			name: "Partition with IsAll flag",
			cluster: &values.SlurmCluster{
				PartitionConfiguration: values.PartitionConfiguration{
					Partitions: []slurmv1.Partition{
						{
							Name:        "main",
							NodeSetRefs: []string{"gpu"},
							Config:      "State=UP",
						},
						{
							Name:   "all-nodes",
							IsAll:  true,
							Config: "Default=YES",
						},
					},
				},
				NodeSetList: slurmv1alpha1.NodeSetList{
					Items: []slurmv1alpha1.NodeSet{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "gpu"},
							Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 2},
						},
					},
				},
			},
			expected: "## Structured partitions will be generated by partition config reconciler\nPartitionName=main Nodes=gpu-0,gpu-1 State=UP\nPartitionName=all-nodes Nodes=gpu-0,gpu-1 Default=YES",
		},
		{
			name: "Partition without Config field",
			cluster: &values.SlurmCluster{
				PartitionConfiguration: values.PartitionConfiguration{
					Partitions: []slurmv1.Partition{
						{
							Name:        "simple",
							NodeSetRefs: []string{"cpu"},
						},
					},
				},
				NodeSetList: slurmv1alpha1.NodeSetList{
					Items: []slurmv1alpha1.NodeSet{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "cpu"},
							Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 1},
						},
					},
				},
			},
			expected: "## Structured partitions will be generated by partition config reconciler\nPartitionName=simple Nodes=cpu-0",
		},
		{
			name: "Partition with non-existent NodeSetRef",
			cluster: &values.SlurmCluster{
				PartitionConfiguration: values.PartitionConfiguration{
					Partitions: []slurmv1.Partition{
						{
							Name:        "missing",
							NodeSetRefs: []string{"nonexistent"},
							Config:      "State=UP",
						},
					},
				},
				NodeSetList: slurmv1alpha1.NodeSetList{
					Items: []slurmv1alpha1.NodeSet{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "gpu"},
							Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 1},
						},
					},
				},
			},
			expected: "## Structured partitions will be generated by partition config reconciler",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &renderutils.PropertiesConfig{}
			addStructuredPartitions(res, tt.cluster)
			assert.Equal(t, tt.expected, res.Render())
		})
	}
}
