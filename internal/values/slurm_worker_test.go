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

	result := BuildSlurmWorkerFrom(clusterName, ptr.To(consts.ModeNone), worker, false)

	if !reflect.DeepEqual(result.SlurmNode, worker.SlurmNode) {
		t.Errorf("Expected SlurmNode to be %v, but got %v", *worker.SlurmNode.DeepCopy(), result.SlurmNode)
	}
	if result.SharedMemorySize != sharedMemorySizeValue {
		t.Errorf("Expected SharedMemorySize to be %v, but got %v", sharedMemorySizeValue, result.SharedMemorySize)
	}
}
