package sharedsteps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type workloadRuntime struct {
	*recordingRuntime
	lease, nodes string
	users        string
	deleted      bool
	acquisitions int
}

func newWorkloadRuntime(t *testing.T) (*workloadRuntime, *framework.WorkerSelector) {
	t.Helper()
	r := &workloadRuntime{recordingRuntime: newRecordingRuntime(t, func(command string) (string, error) {
		if strings.Contains(command, "sbatch ") {
			return "42\n", nil
		}
		return "worker-0\n", nil
	})}
	r.controller = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		value := func(key string) string {
			for _, part := range strings.Fields(command) {
				if v, ok := strings.CutPrefix(part, key+"="); ok {
					return strings.Trim(v, "'")
				}
			}
			return ""
		}
		switch {
		case strings.HasPrefix(command, "sinfo "):
			return "worker-0\nworker-1", nil
		case strings.HasPrefix(command, "scontrol -o show node"):
			return "NodeName=worker-0 State=IDLE CPUAlloc=0 Gres=gpu:test:8\nNodeName=worker-1 State=IDLE CPUAlloc=0 Gres=gpu:test:8", nil
		case strings.HasPrefix(command, "scontrol create reservation"):
			r.acquisitions++
			r.lease, r.nodes, r.users = value("ReservationName"), value("Nodes"), "root"
			return "created", nil
		case strings.HasPrefix(command, "scontrol -o show reservation"):
			if r.lease == "" {
				if command == "scontrol -o show reservations" {
					return "No reservations in the system", nil
				}
				return "", errors.New("Invalid reservation name")
			}
			return fmt.Sprintf("ReservationName=%s Nodes=%s Flags=SPEC_NODES,STATIC Users=%s Duration=08:00:00 State=ACTIVE", r.lease, r.nodes, r.users), nil
		case strings.HasPrefix(command, "scontrol show hostnames "):
			return strings.ReplaceAll(strings.Trim(strings.TrimPrefix(command, "scontrol show hostnames "), "'"), ",", "\n"), nil
		case strings.HasPrefix(command, "scontrol update "):
			r.users += "," + value("Users+")
			return "", nil
		case strings.HasPrefix(command, "squeue "):
			return "", nil
		case strings.HasPrefix(command, "scontrol delete "):
			r.lease, r.deleted = "", true
			return "", nil
		}
		return "", fmt.Errorf("unexpected controller command %s", command)
	})
	r.kubectl = framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		switch strings.Join(args[:2], " ") {
		case "get nodesets":
			return `{"items":[{"metadata":{"name":"worker"},"spec":{"replicas":2,"gpu":{"enabled":true},"docker":{"enabled":true}}}]}`, nil
		case "get pods":
			return `{"items":[{"metadata":{"name":"worker-0"},"spec":{"hostname":"worker-0","nodeName":"host"},"status":{"conditions":[{"type":"Ready","status":"True"}]}},{"metadata":{"name":"worker-1"},"spec":{"hostname":"worker-1","nodeName":"host"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`, nil
		case "get nodes":
			return `{"items":[{"metadata":{"name":"host"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`, nil
		}
		return "", fmt.Errorf("unexpected kubectl arguments %v", args)
	})
	slurm, kubectl := framework.NewSlurmClient(r), framework.NewKubectlClient(r)
	return r, framework.NewWorkerSelector(kubectl, slurm, "soperator")
}

func TestOrdinarySelectorsAcquireCompleteSets(t *testing.T) {
	for _, selection := range []string{"Docker", "idle GPU", "same NodeSet"} {
		t.Run(selection, func(t *testing.T) {
			runtime, selector := newWorkloadRuntime(t)
			ctx := framework.WithWorkerReservation(t.Context(), framework.NewReservationManager(), selection)
			var workers []framework.WorkerInfo
			var err error
			if selection == "Docker" {
				workers, err = selectDockerWorkers(ctx, selector, "GPU", 2)
			} else {
				workers, err = selectIdleGPUWorkers(ctx, selector, 2, selection == "same NodeSet")
			}
			require.NoError(t, err)
			require.Len(t, workers, 2)
			assert.Equal(t, strings.Join(framework.WorkerNames(workers), ","), runtime.nodes)
			require.NoError(t, framework.FinishWorkerReservation(ctx, runtime))
			assert.True(t, runtime.deleted)
		})
	}
}

func TestOrdinaryUserSubmissionsCarryReservation(t *testing.T) {
	for _, submission := range []string{"accounting", "interactive", "NCCL", "tmpfs step", "task prolog", "task info"} {
		t.Run(submission, func(t *testing.T) {
			runtime, selector := newWorkloadRuntime(t)
			ctx := framework.WithWorkerReservation(t.Context(), framework.NewReservationManager(), submission)
			count := 1
			if submission == "NCCL" {
				count = 2
			}
			workers, err := selector.PickGPUWorkers(ctx, count)
			require.NoError(t, err)
			lease := runtime.lease
			slurm := framework.NewSlurmClient(runtime)
			switch submission {
			case "accounting":
				s := &Accounting{runtime: runtime, slurm: slurm, worker: workers[0]}
				err = s.submitAccountingSmokeJob(ctx)
				assert.Equal(t, "42", s.job.ID)
				assert.Contains(t, runtime.users, sshTestUserName)
			case "interactive":
				s := &SystemSettings{runtime: runtime, worker: workers[0]}
				err = s.runInteractiveJobs(ctx)
				assert.Len(t, s.interactive, 2)
				assert.Contains(t, runtime.users, sshTestUserName)
			case "NCCL":
				s := &NCCL{runtime: runtime, workers: workers}
				err = s.runNCCLTest(ctx)
				assert.NoError(t, s.runErr)
			case "tmpfs step":
				// An existing allocation must be discovered within this reservation.
				base := runtime.controller
				runtime.controller = framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
					if strings.HasPrefix(command, "squeue ") {
						return "42", nil
					}
					return base.Run(ctx, command)
				})
				s := &JobTmpfs{runtime: runtime, slurm: slurm}
				err = s.step(ctx, framework.SbatchJob{ID: "42"}, "true")
				runtime.controller = base
			case "task prolog":
				s := &TaskPrologEnv{runtime: runtime, worker: workers[0]}
				err = s.runGPUJobFromLogin(ctx)
			case "task info":
				s := &SoperatorUtils{runtime: runtime, worker: workers[0]}
				err = s.runTaskInfoProlog(ctx)
			}
			require.NoError(t, err)
			var bound []string
			for _, command := range runtime.log {
				if strings.Contains(command, "--reservation="+lease) || strings.Contains(command, "SLURM_RESERVATION="+lease) {
					bound = append(bound, command)
				}
			}
			assert.NotEmpty(t, bound, "submission must carry the actual scope's reservation")
			require.NoError(t, framework.FinishWorkerReservation(ctx, runtime))
		})
	}
}

func TestOrdinarySelectorsPreserveUnscopedSelection(t *testing.T) {
	runtime, selector := newWorkloadRuntime(t)
	workers, err := selectDockerWorkers(t.Context(), selector, "GPU", 2)
	require.NoError(t, err)
	assert.Len(t, workers, 2)
	workers, err = selectIdleGPUWorkers(t.Context(), selector, 2, true)
	require.NoError(t, err)
	assert.Len(t, workers, 2)
	assert.Empty(t, runtime.lease)
}

func TestOrdinarySelectionWaitsForConfiguredWorkers(t *testing.T) {
	runtime, selector := newWorkloadRuntime(t)
	base := runtime.controller
	runtime.controller = framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		out, err := base.Run(ctx, command)
		if strings.HasPrefix(command, "scontrol -o show node") {
			out = strings.ReplaceAll(out, "State=IDLE", "State=DOWN")
		}
		return out, err
	})
	ctx := framework.WithWorkerAcquireTimeout(t.Context(), 20*time.Millisecond)
	ctx = framework.WithWorkerReservation(ctx, framework.NewReservationManager(), t.Name())
	_, err := selectIdleGPUWorkers(ctx, selector, 2, true)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.False(t, framework.IsInsufficientWorkers(err))
	assert.Empty(t, runtime.lease)
	require.NoError(t, framework.FinishWorkerReservation(ctx, runtime))
}

func TestPartitionSmokeJobsShareCompleteAcquisition(t *testing.T) {
	runtime, selector := newWorkloadRuntime(t)
	ctx := framework.WithWorkerReservation(t.Context(), framework.NewReservationManager(), t.Name())
	s := NewClusterCreation(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, framework.NewKubectlClient(runtime), selector)
	require.NoError(t, s.checkMainSmokeJob(ctx))
	require.NoError(t, s.checkHiddenSmokeJob(ctx))
	require.NoError(t, s.checkNodeSetSmokeJobs(ctx))
	assert.Equal(t, 1, runtime.acquisitions)
	assert.Equal(t, "worker-0,worker-1", runtime.nodes)
	commands := strings.Join(runtime.log, "\n")
	assert.Contains(t, commands, "'--partition=hidden'")
	assert.Equal(t, 1, strings.Count(commands, "--partition="), "default smoke must retain default partition selection")
	require.NoError(t, framework.FinishWorkerReservation(ctx, runtime))
}

func TestDockerCleanupWithoutJobIDStopsSubmitterBeforeRelease(t *testing.T) {
	runtime, selector := newWorkloadRuntime(t)
	ctx := framework.WithWorkerReservation(t.Context(), framework.NewReservationManager(), t.Name())
	workers, err := selector.PickGPUWorkers(ctx, 2)
	require.NoError(t, err)
	lease := runtime.lease
	for _, worker := range workers {
		runtime.workers[worker.Name] = framework.NewCommandScope(func(context.Context, string) (string, error) { return "", nil })
	}
	done := make(chan struct{})
	s := &DockerContainers{runtime: runtime, slurm: framework.NewSlurmClient(runtime), workers: workers,
		job: framework.SbatchJob{JobName: "unique-submission"}, containerNamePrefix: "owned-", srunDone: done,
		srunCancel: func() { close(done) },
	}
	s.CleanupAndReset(ctx)
	assert.Contains(t, runtime.log, "jail: scancel --reservation="+framework.ShellQuote(lease))
	assert.NotContains(t, strings.Join(runtime.log, "\n"), "scancel --name=")
	select {
	case <-done:
	default:
		t.Fatal("cleanup returned before submitter stopped")
	}
	require.NoError(t, framework.FinishWorkerReservation(ctx, runtime))
	assert.True(t, runtime.deleted)
}

func TestRequiredWorkloadCleanupFailureRetainsReservation(t *testing.T) {
	runtime, selector := newWorkloadRuntime(t)
	ctx := framework.WithWorkerReservation(t.Context(), framework.NewReservationManager(), t.Name())
	_, err := selector.PickGPUWorkers(ctx, 1)
	require.NoError(t, err)
	runtime.workers["worker-0"] = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		if strings.Contains(command, "docker ps -a") {
			return "owned-container", nil
		}
		if strings.Contains(command, "docker rm") {
			return "", errors.New("Docker daemon unavailable")
		}
		return "", nil
	})
	s := &DockerContainers{runtime: runtime, slurm: framework.NewSlurmClient(runtime),
		workers: []framework.WorkerInfo{{Name: "worker-0"}}, containerNamePrefix: "owned-"}
	s.CleanupAndReset(ctx)
	require.ErrorContains(t, framework.FinishWorkerReservation(ctx, runtime), "Docker daemon unavailable")
	assert.False(t, runtime.deleted)
}

func TestDockerCleanupDistinguishesAbsenceFromFailure(t *testing.T) {
	for _, message := range []string{"No such container: owned", "permission denied", "Docker daemon unavailable"} {
		t.Run(message, func(t *testing.T) {
			runtime := newRecordingRuntimeWithWorker(t, nil, nil, func(string) (string, error) {
				return "", errors.New(message)
			})
			err := runDockerContainerAction(t.Context(), runtime, framework.WorkerInfo{Name: "worker-0"}, "rm -f", "owned")
			if strings.Contains(message, "No such container") {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, message)
			}
		})
	}
}
