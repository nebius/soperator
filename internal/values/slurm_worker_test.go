package values

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

func TestBuildSlurmWorkerFrom(t *testing.T) {
	clusterName := "test-cluster"

	sharedMemorySizeValue := resource.NewQuantity(1, resource.DecimalSI)

	worker := &slurmv1.SlurmNodeWorker{
		Volumes: slurmv1.SlurmNodeWorkerVolumes{
			SharedMemorySize: sharedMemorySizeValue,
		},
	}
	ncclSettings := &slurmv1.NCCLSettings{}

	result := buildSlurmWorkerFrom(clusterName, ptr.To(consts.ModeNone), worker, ncclSettings, false)

	if !reflect.DeepEqual(result.SlurmNode, worker.SlurmNode) {
		t.Errorf("Expected SlurmNode to be %v, but got %v", *worker.SlurmNode.DeepCopy(), result.SlurmNode)
	}
	if result.NCCLSettings != *ncclSettings.DeepCopy() {
		t.Errorf("Expected NCCLSettings to be %v, but got %v", *ncclSettings.DeepCopy(), result.NCCLSettings)
	}
	if result.SharedMemorySize != sharedMemorySizeValue {
		t.Errorf("Expected SharedMemorySize to be %v, but got %v", sharedMemorySizeValue, result.SharedMemorySize)
	}
}
