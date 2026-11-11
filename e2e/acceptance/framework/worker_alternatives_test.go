package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenericSnapshotSelectionPrefersCPUAndUsesMinimumGPUFallback(t *testing.T) {
	for _, count := range []int{1, 2, 3} {
		snapshot := WorkerSnapshot{
			NodeSets: []NodeSetInfo{{Name: "cpu", Replicas: 2}, {Name: "gpu", Replicas: 2, HasGPU: true}},
			Workers:  reservationWorkers("gpu0", "cpu0", "gpu1", "cpu1"),
		}
		workers, err := snapshot.PickWorkers(count)
		require.NoError(t, err)
		var gpuCount int
		for _, worker := range workers {
			if worker.HasGPU {
				gpuCount++
			}
		}
		assert.Equal(t, max(0, count-2), gpuCount)
	}
}
