package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
ConstrainSwapSpace=yes
CgroupPlugin=cgroup/v1`
	resV1 := generateCGroupConfig(clusterV1)
	assert.Equal(t, expectedV1, resV1.Render())

}
