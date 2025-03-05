package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
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
		topologyConfig           v1.ConfigMap
		expectedTopologyPlugin   string
		unexpectedTopologyPlugin string
	}{
		{
			name: "No topology config",
			cluster: values.SlurmCluster{
				SlurmConfig: slurmv1.SlurmConfig{
					TopologyPlugin: "",
				},
				SlurmTopologyConfigMapRefName: "",
			},
			topologyConfig:           v1.ConfigMap{},
			expectedTopologyPlugin:   "",
			unexpectedTopologyPlugin: "",
		},
		{
			name: "Default topology config",
			cluster: values.SlurmCluster{
				SlurmConfig: slurmv1.SlurmConfig{
					TopologyPlugin: "",
				},
				SlurmTopologyConfigMapRefName: "foo",
			},
			topologyConfig: v1.ConfigMap{
				Data: map[string]string{
					consts.ConfigMapKeyTopologyConfig: "# foo",
				},
			},
			expectedTopologyPlugin:   "topology/tree",
			unexpectedTopologyPlugin: "",
		},
		{
			name: "Override topology plugin",
			cluster: values.SlurmCluster{
				SlurmConfig: slurmv1.SlurmConfig{
					TopologyPlugin: "topology/block",
				},
				SlurmTopologyConfigMapRefName: "foo",
			},
			topologyConfig: v1.ConfigMap{
				Data: map[string]string{
					consts.ConfigMapKeyTopologyConfig: "# foo",
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
				SlurmTopologyConfigMapRefName: "foo",
			},
			topologyConfig: v1.ConfigMap{
				Data: map[string]string{},
			},
			expectedTopologyPlugin:   "",
			unexpectedTopologyPlugin: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			result, err := RenderConfigMapSlurmConfigs(&tt.cluster, tt.topologyConfig)
			assert.Nil(t, err)
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
