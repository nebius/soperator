package sharedsteps

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type passiveCheckRuntime struct {
	rolloutPollRuntime
	worker      framework.CommandScope
	workerPod   framework.CommandScope
	jail        framework.CommandScope
	kubectl     framework.ArgsScope
	selectedPod framework.WorkerPodInfo
}

func (r *passiveCheckRuntime) Worker(framework.WorkerInfo) framework.CommandScope { return r.worker }
func (r *passiveCheckRuntime) Jail() framework.CommandScope                       { return r.jail }
func (r *passiveCheckRuntime) Kubectl() framework.ArgsScope                       { return r.kubectl }
func (r *passiveCheckRuntime) WorkerPod(pod framework.WorkerPodInfo) framework.CommandScope {
	r.selectedPod = pod
	return r.workerPod
}

func TestGPUHealthCheckProgramRequiresFreshReportAndRawOutputs(t *testing.T) {
	const oldReport = `{"status":"PASS","meta":{"run_id":"hc-old"}}`
	const freshReport = `{"status":"PASS","meta":{"run_id":"hc-new"}}`
	for _, baseline := range []string{oldReport, ""} {
		t.Run("baseline="+baseline, func(t *testing.T) {
			reads, runs := 0, 0
			var rawCommands []string
			runtime := &passiveCheckRuntime{
				kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
					require.Equal(t, []string{"get", "pods", "-n", "soperator", "-l", "slurm.nebius.ai/worker=true", "-o", "json"}, args)
					return `{"items":[{"metadata":{"name":"worker-pod-0"},"spec":{"hostname":"worker-0"}}]}`, nil
				}),
				jail: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
					require.Equal(t, "scontrol show config | grep -E 'HealthCheckProgram|HealthCheckInterval'", command)
					return "HealthCheckProgram = /opt/slurm_scripts/hc_program.sh\nHealthCheckInterval = 120", nil
				}),
				workerPod: framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
					require.Equal(t, "SLURMD_NODENAME='worker-0' /opt/slurm_scripts/hc_program.sh", command)
					deadline, bounded := ctx.Deadline()
					require.True(t, bounded)
					assert.WithinDuration(t, time.Now().Add(passiveHCProgramTimeout), deadline, time.Second)
					runs++
					return "", nil
				}),
				worker: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
					switch {
					case strings.HasPrefix(command, "test -f "):
						require.Contains(t, command, "/opt/soperator-outputs/local/slurm_scripts/worker-0.gpu_health_check.hc_program.out")
						reads++
						switch reads {
						case 1:
							require.Zero(t, runs, "capture the baseline before running HealthCheckProgram")
							return baseline, nil
						case 2:
							return baseline, nil
						case 3:
							return `{"status":"PASS","meta":`, nil
						default:
							return freshReport, nil
						}
					case strings.HasPrefix(command, "find "):
						rawCommands = append(rawCommands, command)
						return "/opt/soperator-outputs/local/health_checker_cmd_stdout/raw.out", nil
					default:
						t.Fatalf("unexpected worker command: %s", command)
						return "", nil
					}
				}),
			}
			steps := PassiveChecks{
				info:            &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"},
				runtime:         runtime,
				gpuWorker:       framework.WorkerInfo{Name: "worker-0"},
				gpuHealthRunIDs: []string{"prolog", "epilog"},
			}
			require.NoError(t, steps.healthCheckProgramRunsOnTheSelectedGPUWorker(t.Context()))
			assert.Equal(t, "worker-pod-0", runtime.selectedPod.PodName)
			assert.Equal(t, "worker-0", runtime.selectedPod.SlurmNodeName)
			require.NoError(t, steps.theGPUHealthCheckProgramReportIsFreshAndPassing(t.Context()))
			assert.Equal(t, 1, runs)
			assert.Equal(t, 4, reads)
			assert.Equal(t, []string{"prolog", "epilog", "hc-new"}, steps.gpuHealthRunIDs)
			require.NoError(t, steps.rawGPUHealthCheckCommandOutputsArePresent(t.Context()))
			require.Len(t, rawCommands, 3)
			for i, id := range steps.gpuHealthRunIDs {
				assert.Contains(t, rawCommands[i], "*."+id+".out")
			}
		})
	}
}

func TestWaitForPassingHealthCheckReportsRejectsInvalidReports(t *testing.T) {
	for _, tt := range []struct {
		name   string
		output string
		jobID  string
	}{
		{name: "stale passing report", output: `{"status":"PASS","meta":{"run_id":"old"}}`},
		{name: "fresh failing report", output: `{"status":"FAIL","meta":{"run_id":"new"}}`},
		{name: "missing run ID", output: `{"status":"PASS","meta":{}}`},
		{name: "incomplete report", output: `{"status":"PASS","meta":`},
		{name: "malformed report", output: `{invalid}`},
		{name: "missing report"},
		{name: "different job", output: `{"status":"PASS","meta":{"run_id":"new","environment":"SLURM_JOB_ID=42"}}`, jobID: "43"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &passiveCheckRuntime{worker: framework.NewCommandScope(func(context.Context, string) (string, error) {
				return tt.output, nil
			})}
			ids, err := waitForPassingHealthCheckReports(t.Context(), runtime, framework.WorkerInfo{Name: "worker-0"}, "/report.out", tt.jobID, []string{"old"}, time.Minute)
			require.Error(t, err)
			assert.Empty(t, ids)
			if tt.output != "" {
				assert.Contains(t, err.Error(), tt.output)
			}
		})
	}
}

func TestGPUHealthCheckProgramRequiresSlurmConfiguration(t *testing.T) {
	for _, config := range []string{
		"HealthCheckProgram = (null)\nHealthCheckInterval = 120",
		"HealthCheckProgram = /opt/slurm_scripts/hc_program.sh\nHealthCheckInterval = 0",
	} {
		t.Run(config, func(t *testing.T) {
			steps := PassiveChecks{
				gpuWorker: framework.WorkerInfo{Name: "worker-0"},
				runtime: &passiveCheckRuntime{jail: framework.NewCommandScope(func(context.Context, string) (string, error) {
					return config, nil
				})},
			}
			require.ErrorContains(t, steps.healthCheckProgramRunsOnTheSelectedGPUWorker(t.Context()), "is not configured")
		})
	}
}

func TestGPUHealthCheckProgramExecutionIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runtime := &passiveCheckRuntime{
			kubectl: framework.NewArgsScope(func(context.Context, ...string) (string, error) {
				return `{"items":[{"metadata":{"name":"worker-pod-0"},"spec":{"hostname":"worker-0"}}]}`, nil
			}),
			jail: framework.NewCommandScope(func(context.Context, string) (string, error) {
				return "HealthCheckProgram = /opt/slurm_scripts/hc_program.sh\nHealthCheckInterval = 120", nil
			}),
			worker: framework.NewCommandScope(func(context.Context, string) (string, error) {
				return "", nil
			}),
			workerPod: framework.NewCommandScope(func(ctx context.Context, _ string) (string, error) {
				<-ctx.Done()
				return "", ctx.Err()
			}),
		}
		steps := PassiveChecks{
			info:      &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"},
			runtime:   runtime,
			gpuWorker: framework.WorkerInfo{Name: "worker-0"},
		}
		started := time.Now()
		require.ErrorIs(t, steps.healthCheckProgramRunsOnTheSelectedGPUWorker(t.Context()), context.DeadlineExceeded)
		assert.Equal(t, passiveHCProgramTimeout, time.Since(started))
	})
}

func TestRunManualHCProgramRequiresMatchingWorkerPod(t *testing.T) {
	runtime := &passiveCheckRuntime{
		kubectl: framework.NewArgsScope(func(context.Context, ...string) (string, error) {
			return `{"items":[{"metadata":{"name":"other-pod"},"spec":{"hostname":"other-worker"}}]}`, nil
		}),
	}
	err := runManualHCProgram(t.Context(), runtime, framework.WorkerInfo{Name: "worker-0"})
	require.ErrorContains(t, err, `worker pod for Slurm node "worker-0" was not found`)
}
