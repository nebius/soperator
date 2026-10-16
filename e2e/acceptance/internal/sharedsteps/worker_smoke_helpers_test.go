package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestTargetedWorkerSmokeCommand(t *testing.T) {
	tests := []struct {
		name     string
		worker   framework.WorkerInfo
		contains []string
		excludes string
	}{
		{
			name:     "CPU",
			worker:   framework.WorkerInfo{Name: "worker-cpu-0"},
			contains: []string{"timeout 180", "srun -w 'worker-cpu-0'", "hostname"},
			excludes: "--gpus-per-node",
		},
		{
			name:     "GPU",
			worker:   framework.WorkerInfo{Name: "worker-gpu-0", HasGPU: true},
			contains: []string{"timeout 180", "srun -w 'worker-gpu-0'", "--gpus-per-node=1", "nvidia-smi -L"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, err := targetedWorkerSmokeCommand(tt.worker)
			require.NoError(t, err)
			for _, expected := range tt.contains {
				assert.Contains(t, command, expected)
			}
			if tt.excludes != "" {
				assert.NotContains(t, command, tt.excludes)
			}
		})
	}
}
