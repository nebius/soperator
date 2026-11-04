package sharedsteps

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type workerSmokeTestRuntime struct {
	framework.Runtime
	controller framework.CommandScope
	jail       framework.CommandScope
}

func (r workerSmokeTestRuntime) Controller() framework.CommandScope { return r.controller }
func (r workerSmokeTestRuntime) Jail() framework.CommandScope       { return r.jail }

func TestValidateWorkersOperationalRejectsMissingOrUnusableStateBeforeSmoke(t *testing.T) {
	tests := []struct {
		name    string
		workers []framework.WorkerInfo
		output  string
		wantErr string
	}{
		{name: "no workers", wantErr: "no workers were recorded"},
		{name: "missing state", workers: []framework.WorkerInfo{{Name: "worker-0"}},
			output: "NodeName=worker-0", wantErr: "parse worker node state worker-0: no State field"},
		{name: "maintenance drain", workers: []framework.WorkerInfo{{Name: "worker-0"}},
			output:  "NodeName=worker-0 State=IDLE+DRAIN Reason=maintenance",
			wantErr: "worker node worker-0 has bad state=IDLE+DRAIN reason=maintenance"},
		{name: "down", workers: []framework.WorkerInfo{{Name: "worker-0"}},
			output: "NodeName=worker-0 State=DOWN", wantErr: "worker node worker-0 has bad state=DOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reads, smokeRuns int
			runtime := workerSmokeTestRuntime{
				controller: framework.NewCommandScope(func(context.Context, string) (string, error) {
					reads++
					return tt.output, nil
				}),
				jail: framework.NewCommandScope(func(context.Context, string) (string, error) {
					smokeRuns++
					return "", nil
				}),
			}
			err := validateWorkersOperational(t.Context(), runtime, framework.NewSlurmClient(runtime), tt.workers)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Equal(t, len(tt.workers), reads)
			assert.Zero(t, smokeRuns, "smoke must not run until the worker state is validated")
		})
	}
}

func TestValidateWorkersOperationalTargetsEachWorkerThroughScopedRuntime(t *testing.T) {
	for _, throughStep := range []bool{false, true} {
		t.Run(fmt.Sprintf("node replacement step=%t", throughStep), func(t *testing.T) {
			var calls []string
			runtime := workerSmokeTestRuntime{
				controller: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
					calls = append(calls, "controller: "+command)
					return "State=IDLE+CLOUD", nil
				}),
				jail: framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
					deadline, bounded := ctx.Deadline()
					require.True(t, bounded, "each smoke job has a deadline")
					assert.WithinDuration(t, time.Now().Add(framework.TargetedWorkerSmokeTimeout), deadline, time.Second)
					calls = append(calls, "jail: "+command)
					return "", nil
				}),
			}
			slurm := framework.NewSlurmClient(runtime)
			workers := []framework.WorkerInfo{{Name: "worker-cpu-0"}, {Name: "worker-gpu-0", HasGPU: true}}
			var err error
			if throughStep {
				steps := NodeReplacement{runtime: runtime, slurm: slurm, preExistingWorkers: workers}
				err = steps.allPreExistingWorkerNodesAreOperational(t.Context())
			} else {
				err = validateWorkersOperational(t.Context(), runtime, slurm, workers)
			}
			require.NoError(t, err)
			assert.Equal(t, []string{
				"controller: scontrol show node 'worker-cpu-0'",
				"jail: timeout 180 srun -w 'worker-cpu-0' hostname",
				"controller: scontrol show node 'worker-gpu-0'",
				"jail: timeout 180 srun -w 'worker-gpu-0' --gpus-per-node=1 nvidia-smi -L >/dev/null",
			}, calls)
		})
	}
}

func TestValidateWorkersOperationalPropagatesSmokeFailure(t *testing.T) {
	smokeErr := errors.New("targeted job exited unsuccessfully")
	runtime := workerSmokeTestRuntime{
		controller: framework.NewCommandScope(func(context.Context, string) (string, error) {
			return "State=IDLE+CLOUD", nil
		}),
		jail: framework.NewCommandScope(func(context.Context, string) (string, error) {
			return "", smokeErr
		}),
	}
	err := validateWorkersOperational(t.Context(), runtime, framework.NewSlurmClient(runtime),
		[]framework.WorkerInfo{{Name: "worker-0"}})
	assert.ErrorIs(t, err, smokeErr)
	assert.ErrorContains(t, err, "validate worker worker-0 is operational")
}
