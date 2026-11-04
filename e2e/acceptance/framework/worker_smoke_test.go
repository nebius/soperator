package framework_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestRunTargetedWorkerSmoke(t *testing.T) {
	for _, tt := range []struct {
		name   string
		worker framework.WorkerInfo
		want   string
	}{
		{
			name: "CPU", worker: framework.WorkerInfo{Name: "worker-cpu-0"},
			want: "timeout 180 srun -w 'worker-cpu-0' hostname",
		},
		{
			name: "GPU", worker: framework.WorkerInfo{Name: "worker-gpu-0", HasGPU: true},
			want: "timeout 180 srun -w 'worker-gpu-0' --gpus-per-node=1 nvidia-smi -L >/dev/null",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime := workerSmokeRuntime{jail: framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
				assert.Equal(t, tt.want, command)
				deadline, bounded := ctx.Deadline()
				require.True(t, bounded)
				assert.WithinDuration(t, time.Now().Add(framework.TargetedWorkerSmokeTimeout), deadline, time.Second)
				return "", nil
			})}

			require.NoError(t, framework.RunTargetedWorkerSmoke(t.Context(), runtime, tt.worker))
		})
	}
}

func TestRunTargetedWorkerSmokeRejectsEmptyWorker(t *testing.T) {
	err := framework.RunTargetedWorkerSmoke(t.Context(), workerSmokeRuntime{}, framework.WorkerInfo{})
	require.ErrorContains(t, err, "worker name is empty")
}

func TestRunTargetedWorkerSmokePreservesCommandFailure(t *testing.T) {
	commandErr := errors.New("GPU validation failed")
	runtime := workerSmokeRuntime{jail: framework.NewCommandScope(func(context.Context, string) (string, error) {
		return "", commandErr
	})}

	err := framework.RunTargetedWorkerSmoke(t.Context(), runtime, framework.WorkerInfo{Name: "worker-0", HasGPU: true})

	require.ErrorIs(t, err, commandErr)
	assert.ErrorContains(t, err, "worker worker-0")
}

func TestRunTargetedWorkerSmokeBoundsExecution(t *testing.T) {
	for _, timeout := range []time.Duration{time.Minute, framework.TargetedWorkerSmokeTimeout + time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), timeout)
				defer cancel()
				runtime := workerSmokeRuntime{jail: framework.NewCommandScope(func(ctx context.Context, _ string) (string, error) {
					<-ctx.Done()
					return "", ctx.Err()
				})}
				start := time.Now()

				err := framework.RunTargetedWorkerSmoke(ctx, runtime, framework.WorkerInfo{Name: "worker-0"})

				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Equal(t, min(timeout, framework.TargetedWorkerSmokeTimeout), time.Since(start))
			})
		})
	}
}

type workerSmokeRuntime struct {
	framework.Runtime
	jail framework.CommandScope
}

func (r workerSmokeRuntime) Jail() framework.CommandScope { return r.jail }
