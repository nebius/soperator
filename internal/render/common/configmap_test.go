package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
	t.Run("default config v2", func(t *testing.T) {
		cluster := &values.SlurmCluster{
			NodeWorker: values.SlurmWorker{CgroupVersion: consts.CGroupV2},
		}
		got := generateCGroupConfig(cluster).Render()
		want := `CgroupMountpoint=/sys/fs/cgroup
ConstrainCores=yes
ConstrainDevices=yes
ConstrainRAMSpace=yes
CgroupPlugin=cgroup/v2
ConstrainSwapSpace=no
EnableControllers=yes
IgnoreSystemd=yes`
		assert.Equal(t, want, got)
	})

	t.Run("default config v1", func(t *testing.T) {
		cluster := &values.SlurmCluster{
			NodeWorker: values.SlurmWorker{CgroupVersion: consts.CGroupV1},
		}
		got := generateCGroupConfig(cluster).Render()
		want := `CgroupMountpoint=/sys/fs/cgroup
ConstrainCores=yes
ConstrainDevices=yes
ConstrainRAMSpace=yes
CgroupPlugin=cgroup/v1
ConstrainSwapSpace=yes`
		assert.Equal(t, want, got)
	})

	t.Run("custom overrides defaults", func(t *testing.T) {
		customConfig := "ConstrainCores=no\nAllowedKmemSpace=yes"
		cluster := &values.SlurmCluster{
			NodeWorker:         values.SlurmWorker{CgroupVersion: consts.CGroupV2},
			CustomCgroupConfig: &customConfig,
		}
		got := generateCGroupConfig(cluster).Render()
		want := `CgroupMountpoint=/sys/fs/cgroup
ConstrainDevices=yes
ConstrainRAMSpace=yes
CgroupPlugin=cgroup/v2
ConstrainSwapSpace=no
EnableControllers=yes
IgnoreSystemd=yes
###
# Custom config
###
ConstrainCores=no
AllowedKmemSpace=yes`
		assert.Equal(t, want, got)
	})

	t.Run("custom config with comments", func(t *testing.T) {
		customConfig := "# This is a comment\nConstrainCores=no"
		cluster := &values.SlurmCluster{
			NodeWorker:         values.SlurmWorker{CgroupVersion: consts.CGroupV2},
			CustomCgroupConfig: &customConfig,
		}
		got := generateCGroupConfig(cluster).Render()
		want := `CgroupMountpoint=/sys/fs/cgroup
ConstrainDevices=yes
ConstrainRAMSpace=yes
CgroupPlugin=cgroup/v2
ConstrainSwapSpace=no
EnableControllers=yes
IgnoreSystemd=yes
###
# Custom config
###
# This is a comment
ConstrainCores=no`
		assert.Equal(t, want, got)
	})

	t.Run("custom config with empty lines between entries", func(t *testing.T) {
		customConfig := "ConstrainCores=no\n   \nAllowedKmemSpace=yes"
		cluster := &values.SlurmCluster{
			NodeWorker:         values.SlurmWorker{CgroupVersion: consts.CGroupV2},
			CustomCgroupConfig: &customConfig,
		}
		got := generateCGroupConfig(cluster).Render()
		want := `CgroupMountpoint=/sys/fs/cgroup
ConstrainDevices=yes
ConstrainRAMSpace=yes
CgroupPlugin=cgroup/v2
ConstrainSwapSpace=no
EnableControllers=yes
IgnoreSystemd=yes
###
# Custom config
###
ConstrainCores=no
AllowedKmemSpace=yes`
		assert.Equal(t, want, got)
	})

	t.Run("custom config with malformed lines", func(t *testing.T) {
		customConfig := "This line lacks equals"
		cluster := &values.SlurmCluster{
			NodeWorker:         values.SlurmWorker{CgroupVersion: consts.CGroupV2},
			CustomCgroupConfig: &customConfig,
		}
		got := generateCGroupConfig(cluster).Render()
		want := `CgroupMountpoint=/sys/fs/cgroup
ConstrainCores=yes
ConstrainDevices=yes
ConstrainRAMSpace=yes
CgroupPlugin=cgroup/v2
ConstrainSwapSpace=no
EnableControllers=yes
IgnoreSystemd=yes
###
# Custom config
###
This line lacks equals`
		assert.Equal(t, want, got)
	})

	t.Run("empty custom config does not add block", func(t *testing.T) {
		customConfig := "  \n\t"
		cluster := &values.SlurmCluster{
			NodeWorker:         values.SlurmWorker{CgroupVersion: consts.CGroupV2},
			CustomCgroupConfig: &customConfig,
		}
		got := generateCGroupConfig(cluster).Render()
		want := `CgroupMountpoint=/sys/fs/cgroup
ConstrainCores=yes
ConstrainDevices=yes
ConstrainRAMSpace=yes
CgroupPlugin=cgroup/v2
ConstrainSwapSpace=no
EnableControllers=yes
IgnoreSystemd=yes`
		assert.Equal(t, want, got)
	})

	t.Run("custom config adds new keys without overriding defaults", func(t *testing.T) {
		customConfig := "AllowedKmemSpace=yes"
		cluster := &values.SlurmCluster{
			NodeWorker:         values.SlurmWorker{CgroupVersion: consts.CGroupV2},
			CustomCgroupConfig: &customConfig,
		}
		got := generateCGroupConfig(cluster).Render()
		want := `CgroupMountpoint=/sys/fs/cgroup
ConstrainCores=yes
ConstrainDevices=yes
ConstrainRAMSpace=yes
CgroupPlugin=cgroup/v2
ConstrainSwapSpace=no
EnableControllers=yes
IgnoreSystemd=yes
###
# Custom config
###
AllowedKmemSpace=yes`
		assert.Equal(t, want, got)
	})
}

func Test_parseCGroupKV(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		wantOK bool
		want   string
	}{
		{name: "spaces around equals", line: " key = value ", wantOK: true, want: "key"},
		{name: "empty value", line: "key=", wantOK: true, want: "key"},
		{name: "multiple equals", line: "key=value=extra", wantOK: true, want: "key"},
		{name: "comment line", line: "# comment", wantOK: false, want: ""},
		{name: "whitespace only", line: "   \t", wantOK: false, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseCGroupKV(tt.line)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
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

func Test_RenderRealMemorySlurmd(t *testing.T) {
	tests := []struct {
		name           string
		container      corev1.ResourceRequirements
		resourceMemory string // Original memory resource as a string (e.g., "512Mi", "1G")
		expectedValue  int64  // Expected memory value in mebibytes
	}{
		{
			name: "Valid memory resource - 512Mi",
			container: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			},
			resourceMemory: "512Mi", // Input memory value
			expectedValue:  512,     // Expected value in MiB
		},
		{
			name: "Valid memory resource - 1G",
			container: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1G"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1G"),
				},
			},
			resourceMemory: "1G", // Input memory value
			expectedValue:  953,  // Expected value in MiB (1G = 1024MB, 1024MB / 1.048576 = 953MiB)
		},
		{
			name: "Valid memory resource - 1000MB",
			container: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1000M"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1000M"),
				},
			},
			resourceMemory: "1000M", // Input memory value
			expectedValue:  953,     // Expected value in MiB (1000MB / 1.048576 = 953MiB)
		},
		{
			name:           "No memory resource",
			container:      corev1.ResourceRequirements{},
			resourceMemory: "", // No memory specified
			expectedValue:  0,  // Expected value in MiB
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function under test
			value := RenderRealMemorySlurmd(tt.container)

			// Validate the value
			if value != tt.expectedValue {
				t.Errorf("renderRealMemorySlurmd() = %v, expectedValue %v (from %s)", value, tt.expectedValue, tt.resourceMemory)
			}
		})
	}
}

func TestAddNodesToSlurmConfig(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *values.SlurmCluster
		expected string
	}{
		{
			name: "Single nodeset with 1 replica",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				NodeSets: []slurmv1alpha1.NodeSet{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "nodeA",
							Namespace: "soperator",
						},
						Spec: slurmv1alpha1.NodeSetSpec{
							Replicas: 1,
							Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
								Resources: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
							NodeConfig: slurmv1alpha1.NodeConfig{
								Features: []string{"a", "b"},
								Static:   "Gres=gpu:nvidia-a100:4 NodeCPUs=64 Boards=1 SocketsPerBoard=2 CoresPerSocket=32 ThreadsPerCode=1 Feature=c,d",
							},
						},
					},
				},
			},
			expected: "NodeName=nodeA-0 NodeHostname=nodeA-0 NodeAddr=nodeA-0.slurm-test-nodeset-svc.soperator.svc.cluster.local RealMemory=2048 Feature=a,b Gres=gpu:nvidia-a100:4 NodeCPUs=64 Boards=1 SocketsPerBoard=2 CoresPerSocket=32 ThreadsPerCode=1",
		},
		{
			name: "Single nodeset with multiple replicas",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				NodeSets: []slurmv1alpha1.NodeSet{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "nodeB",
							Namespace: "soperator",
						},
						Spec: slurmv1alpha1.NodeSetSpec{
							Replicas: 3,
							Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
								Resources: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("4Gi"),
								},
							},
							NodeConfig: slurmv1alpha1.NodeConfig{
								Static: "Gres=gpu:nvidia-a100:8 NodeCPUs=128 Boards=1 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1",
							},
						},
					},
				},
			},
			expected: "NodeName=nodeB-0 NodeHostname=nodeB-0 NodeAddr=nodeB-0.slurm-test-nodeset-svc.soperator.svc.cluster.local RealMemory=4096 Gres=gpu:nvidia-a100:8 NodeCPUs=128 Boards=1 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1\n" +
				"NodeName=nodeB-1 NodeHostname=nodeB-1 NodeAddr=nodeB-1.slurm-test-nodeset-svc.soperator.svc.cluster.local RealMemory=4096 Gres=gpu:nvidia-a100:8 NodeCPUs=128 Boards=1 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1\n" +
				"NodeName=nodeB-2 NodeHostname=nodeB-2 NodeAddr=nodeB-2.slurm-test-nodeset-svc.soperator.svc.cluster.local RealMemory=4096 Gres=gpu:nvidia-a100:8 NodeCPUs=128 Boards=1 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1",
		},
		{
			name: "Multiple nodesets with varying replicas",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				NodeSets: []slurmv1alpha1.NodeSet{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "nodeC",
							Namespace: "soperator",
						},
						Spec: slurmv1alpha1.NodeSetSpec{
							Replicas: 2,
							Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
								Resources: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
							NodeConfig: slurmv1alpha1.NodeConfig{
								Static: "Gres=gpu:nvidia-a100:16 NodeCPUs=256 Boards=2 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "nodeD",
							Namespace: "soperator",
						},
						Spec: slurmv1alpha1.NodeSetSpec{
							Replicas: 1,
							Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
								Resources: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("16Gi"),
								},
							},
							NodeConfig: slurmv1alpha1.NodeConfig{
								Static: "Gres=gpu:nvidia-a100:32 NodeCPUs=512 Boards=4 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1",
							},
						},
					},
				},
			},
			expected: "NodeName=nodeC-0 NodeHostname=nodeC-0 NodeAddr=nodeC-0.slurm-test-nodeset-svc.soperator.svc.cluster.local RealMemory=8192 Gres=gpu:nvidia-a100:16 NodeCPUs=256 Boards=2 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1\n" +
				"NodeName=nodeC-1 NodeHostname=nodeC-1 NodeAddr=nodeC-1.slurm-test-nodeset-svc.soperator.svc.cluster.local RealMemory=8192 Gres=gpu:nvidia-a100:16 NodeCPUs=256 Boards=2 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1\n" +
				"NodeName=nodeD-0 NodeHostname=nodeD-0 NodeAddr=nodeD-0.slurm-test-nodeset-svc.soperator.svc.cluster.local RealMemory=16384 Gres=gpu:nvidia-a100:32 NodeCPUs=512 Boards=4 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1",
		},
		{
			name: "Nodeset with zero replicas",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				NodeSets: []slurmv1alpha1.NodeSet{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "nodeE",
							Namespace: "soperator",
						},
						Spec: slurmv1alpha1.NodeSetSpec{
							Replicas: 0,
							Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
								Resources: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
							NodeConfig: slurmv1alpha1.NodeConfig{
								Static: "Gres=gpu:nvidia-a100:4 NodeCPUs=64 Boards=1 SocketsPerBoard=2 CoresPerSocket=32 ThreadsPerCode=1",
							},
						},
					},
				},
			},
			expected: "#WARNING: NodeSet nodeE has 0 replicas, skipping",
		},
		{
			name: "Nodeset with zero replicas",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				NodeSets: []slurmv1alpha1.NodeSet{},
			},
			expected: "#WARNING: No nodesets defined in structured configuration!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &renderutils.PropertiesConfig{}
			AddNodesToSlurmConfig(res, tt.cluster)
			result := res.Render()

			// Check if the expected string is present in the result
			if !strings.Contains(result, tt.expected) {
				t.Errorf("Expected string not found in result.\nExpected to contain:\n%s\n\nGot:\n%s", tt.expected, result)
			}
		})
	}
}

func TestAddNodeSetsToSlurmConfig(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *values.SlurmCluster
		expected string
	}{
		{
			name: "Single nodeset with 1 replica",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				NodeSets: []slurmv1alpha1.NodeSet{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "nodeA",
							Namespace: "soperator",
						},
						Spec: slurmv1alpha1.NodeSetSpec{
							Replicas: 1,
							Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
								Resources: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
							NodeConfig: slurmv1alpha1.NodeConfig{
								Static: "Gres=gpu:nvidia-a100:4 NodeCPUs=64 Boards=1 SocketsPerBoard=2 CoresPerSocket=32 ThreadsPerCode=1",
							},
						},
					},
				},
			},
			expected: "NodeSet=nodeA Nodes=nodeA-0",
		},
		{
			name: "Single nodeset with multiple replicas",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				NodeSets: []slurmv1alpha1.NodeSet{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "nodeB",
							Namespace: "soperator",
						},
						Spec: slurmv1alpha1.NodeSetSpec{
							Replicas: 3,
							Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
								Resources: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("4Gi"),
								},
							},
							NodeConfig: slurmv1alpha1.NodeConfig{
								Static: "Gres=gpu:nvidia-a100:8 NodeCPUs=128 Boards=1 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1",
							},
						},
					},
				},
			},
			expected: "NodeSet=nodeB Nodes=nodeB-[0-2]",
		},
		{
			name: "Multiple nodesets with varying replicas",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				NodeSets: []slurmv1alpha1.NodeSet{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "nodeC",
							Namespace: "soperator",
						},
						Spec: slurmv1alpha1.NodeSetSpec{
							Replicas: 2,
							Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
								Resources: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
							NodeConfig: slurmv1alpha1.NodeConfig{
								Static: "Gres=gpu:nvidia-a100:16 NodeCPUs=256 Boards=2 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "nodeD",
							Namespace: "soperator",
						},
						Spec: slurmv1alpha1.NodeSetSpec{
							Replicas: 1,
							Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
								Resources: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("16Gi"),
								},
							},
							NodeConfig: slurmv1alpha1.NodeConfig{
								Static: "Gres=gpu:nvidia-a100:32 NodeCPUs=512 Boards=4 SocketsPerBoard=4 CoresPerSocket=32 ThreadsPerCode=1",
							},
						},
					},
				},
			},
			expected: "NodeSet=nodeC Nodes=nodeC-[0-1]\n" +
				"NodeSet=nodeD Nodes=nodeD-0",
		},
		{
			name: "Nodeset with zero replicas",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				NodeSets: []slurmv1alpha1.NodeSet{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "nodeE",
							Namespace: "soperator",
						},
						Spec: slurmv1alpha1.NodeSetSpec{
							Replicas: 0,
							Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
								Resources: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
							NodeConfig: slurmv1alpha1.NodeConfig{
								Static: "Gres=gpu:nvidia-a100:4 NodeCPUs=64 Boards=1 SocketsPerBoard=2 CoresPerSocket=32 ThreadsPerCode=1",
							},
						},
					},
				},
			},
			expected: "#WARNING: NodeSet nodeE has 0 replicas, skipping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &renderutils.PropertiesConfig{}
			AddNodeSetsToSlurmConfig(res, tt.cluster)
			result := res.Render()

			// Check if the expected string is present in the result
			if !strings.Contains(result, tt.expected) {
				t.Errorf("Expected string not found in result.\nExpected to contain:\n%s\n\nGot:\n%s", tt.expected, result)
			}
		})
	}
}

func TestAddPartitionsToSlurmConfig(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *values.SlurmCluster
		expected string
	}{
		{
			name: "Single partition with isAll",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				PartitionConfiguration: values.PartitionConfiguration{
					ConfigType: "structured",
					Partitions: []slurmv1.Partition{
						{
							Name:   "main",
							IsAll:  true,
							Config: "Default=YES PriorityTier=10 MaxTime=INFINITE State=UP",
						},
					},
				},
			},
			expected: "PartitionName=main Nodes=ALL Default=YES PriorityTier=10 MaxTime=INFINITE State=UP",
		},
		{
			name: "Single partition with nodeset refs",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				PartitionConfiguration: values.PartitionConfiguration{
					ConfigType: "structured",
					Partitions: []slurmv1.Partition{
						{
							Name:        "gpu",
							NodeSetRefs: []string{"nodeA", "nodeB"},
							Config:      "Default=NO PriorityTier=5 State=UP",
						},
					},
				},
			},
			expected: "PartitionName=gpu Nodes=nodeA,nodeB Default=NO PriorityTier=5 State=UP",
		},
		{
			name: "Multiple partitions with varying configurations",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				PartitionConfiguration: values.PartitionConfiguration{
					ConfigType: "structured",
					Partitions: []slurmv1.Partition{
						{
							Name:        "high-priority",
							NodeSetRefs: []string{"nodeC"},
							Config:      "Default=YES PriorityTier=10 State=UP",
						},
						{
							Name:   "all-nodes",
							IsAll:  true,
							Config: "Default=NO PriorityTier=1 State=UP",
						},
					},
				},
			},
			expected: "PartitionName=all-nodes Nodes=ALL Default=NO PriorityTier=1 State=UP",
		},
		{
			name: "Partition with no nodeset refs and not isAll",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				PartitionConfiguration: values.PartitionConfiguration{
					ConfigType: "structured",
					Partitions: []slurmv1.Partition{
						{
							Name:   "invalid",
							Config: "Default=NO State=UP",
						},
					},
				},
			},
			expected: "#WARNING: Partition invalid has no nodeset refs and is not 'all', skipping",
		},
		{
			name: "No partitions defined",
			cluster: &values.SlurmCluster{
				NamespacedName: types.NamespacedName{
					Namespace: "soperator",
					Name:      "slurm-test",
				},
				PartitionConfiguration: values.PartitionConfiguration{
					ConfigType: "structured",
					Partitions: []slurmv1.Partition{},
				},
			},
			expected: "#WARNING: No partitions defined in structured configuration!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &renderutils.PropertiesConfig{}
			AddPartitionsToSlurmConfig(res, tt.cluster)
			result := res.Render()

			// Check if the expected string is present in the result
			if !strings.Contains(result, tt.expected) {
				t.Errorf("Expected string not found in result.\nExpected to contain:\n%s\n\nGot:\n%s", tt.expected, result)
			}
		})
	}
}
