package sharedsteps

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const passingNVIDIALibrariesLog = "Loaded libcuda.so.1\nLoaded libnvidia-ml.so.1\nAll 3 NVIDIA libraries are loadable\n"

func TestNVIDIALibrariesCheckEnabled(t *testing.T) {
	for _, tt := range []struct {
		name    string
		config  string
		enabled bool
		invalid bool
	}{
		{name: "default filters", config: `[{"name":"nvidia_libraries"}]`, enabled: true},
		{name: "matching hooks and platform", config: `[{"name":"nvidia_libraries","contexts":["prolog","hc_program"],"platforms":["8xGPU"]}]`, enabled: true},
		{name: "any filters", config: `[{"name":"nvidia_libraries","contexts":["any"],"platforms":["any"]}]`, enabled: true},
		{name: "disabled", config: `[]`},
		{name: "other check", config: `[{"name":"gpu_health_check"}]`},
		{name: "missing hook", config: `[{"name":"nvidia_libraries","contexts":["prolog"]}]`},
		{name: "no contexts", config: `[{"name":"nvidia_libraries","contexts":[]}]`},
		{name: "other platform", config: `[{"name":"nvidia_libraries","platforms":["4xGPU"]}]`},
		{name: "only drained nodes", config: `[{"name":"nvidia_libraries","node_states":["drain"]}]`},
		{name: "malformed configuration", config: `{`, invalid: true},
		{name: "null configuration", config: `null`, invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			enabled, err := nvidiaLibrariesCheckEnabled(tt.config, []string{"8xH200", "8xGPU"})
			if tt.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.enabled, enabled)
		})
	}
}

func TestNVIDIALibrariesPrerequisiteSkipsDisabledButFailsInvalidConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name     string
		config   string
		metadata string
		wantSkip bool
		wantErr  string
	}{
		{name: "enabled", config: `[{"name":"nvidia_libraries"}]`, metadata: "SOPERATOR_NODE_PLATFORM_TAGS=8xH200,8xGPU\n"},
		{name: "disabled", config: `[]`, metadata: "SOPERATOR_NODE_PLATFORM_TAGS=8xH200,8xGPU\n", wantSkip: true},
		{name: "invalid config", config: `{`, metadata: "SOPERATOR_NODE_PLATFORM_TAGS=8xH200,8xGPU\n", wantErr: "parse passive-check configuration"},
		{name: "missing metadata", wantErr: "missing metadata"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &passiveCheckRuntime{
				kubectl: framework.NewArgsScope(func(context.Context, ...string) (string, error) {
					return `{"items":[{"metadata":{"name":"worker-pod"},"spec":{"hostname":"worker-0"}}]}`, nil
				}),
				workerPod: framework.NewCommandScope(func(context.Context, string) (string, error) {
					return tt.metadata, nil
				}),
				worker: framework.NewCommandScope(func(context.Context, string) (string, error) {
					return tt.config, nil
				}),
				jail: framework.NewCommandScope(func(context.Context, string) (string, error) {
					return "HealthCheckProgram = /opt/slurm_scripts/hc_program.sh\nHealthCheckInterval = 120", nil
				}),
			}
			steps := PassiveChecks{runtime: runtime, gpuWorker: framework.WorkerInfo{Name: "worker-0"}}
			err := steps.nvidiaLibrariesCheckIsEnabled(t.Context())
			switch {
			case tt.wantSkip:
				require.ErrorIs(t, err, godog.ErrSkip)
			case tt.wantErr != "":
				require.ErrorContains(t, err, tt.wantErr)
				assert.NotErrorIs(t, err, godog.ErrSkip)
			default:
				require.NoError(t, err)
			}
		})
	}
}

func TestAssertNVIDIALibrariesLoadedRejectsFailOpenResults(t *testing.T) {
	for _, output := range []string{passingNVIDIALibrariesLog, strings.ReplaceAll(passingNVIDIALibrariesLog, "\n", "\r\n")} {
		require.NoError(t, assertNVIDIALibrariesLoaded(output))
	}
	for _, tt := range []struct {
		name   string
		output string
	}{
		{name: "empty"},
		{name: "skipped worker", output: "Not a GPU worker, skipping\n"},
		{name: "missing CUDA load", output: strings.ReplaceAll(passingNVIDIALibrariesLog, "Loaded libcuda.so.1\n", "")},
		{name: "missing NVML load", output: strings.ReplaceAll(passingNVIDIALibrariesLog, "Loaded libnvidia-ml.so.1\n", "")},
		{name: "incomplete", output: "Loaded libcuda.so.1\nLoaded libnvidia-ml.so.1\n"},
		{name: "skipped CLI enumeration", output: "nvidia-container-cli exited with 1, skipping the driver library file check:\n" + passingNVIDIALibrariesLog},
		{name: "unhandled error", output: passingNVIDIALibrariesLog + "Unhandled exception, skipping the check\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, assertNVIDIALibrariesLoaded(tt.output))
		})
	}
}

func nvidiaCheckRunnerOutput(jobID string) string {
	return "INFO: Started\nINFO: Environment SLURM_JOB_ID=\"" + jobID + "\"\nINFO: Check nvidia_libraries: OK\nINFO: Finished in 1.000 seconds\n"
}

func TestNVIDIALibrariesPrologRequiresItsJobAndActualCheckExecution(t *testing.T) {
	for _, tt := range []struct {
		name   string
		runner string
		log    string
		valid  bool
	}{
		{name: "matching job", runner: nvidiaCheckRunnerOutput("42"), log: passingNVIDIALibrariesLog, valid: true},
		{name: "stale job", runner: nvidiaCheckRunnerOutput("41"), log: passingNVIDIALibrariesLog},
		{name: "check did not run", runner: strings.ReplaceAll(nvidiaCheckRunnerOutput("42"), "nvidia_libraries", "gpu_health_check"), log: passingNVIDIALibrariesLog},
		{name: "check failed", runner: strings.ReplaceAll(nvidiaCheckRunnerOutput("42"), ": OK", ": FAIL"), log: "libcuda.so.1 is missing\n"},
		{name: "fail-open skip", runner: nvidiaCheckRunnerOutput("42"), log: "Unhandled exception, skipping the check\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &passiveCheckRuntime{worker: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
				if strings.Contains(command, ".check_runner.prolog.out") {
					return tt.runner, nil
				}
				require.Contains(t, command, "worker-0.nvidia_libraries.prolog.out")
				return tt.log, nil
			})}
			steps := PassiveChecks{
				info: &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"}, runtime: runtime,
				gpuWorker: framework.WorkerInfo{Name: "worker-0"}, gpuHooksJob: framework.SbatchJob{ID: "42"},
			}
			err := steps.nvidiaLibrariesPrologCheckPasses(t.Context())
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestNVIDIALibrariesHCProgramRequiresChangedOutputAfterInvocation(t *testing.T) {
	for _, baseline := range []string{"", "INFO: Previous run\n", nvidiaCheckRunnerOutput("")} {
		t.Run("baseline="+baseline, func(t *testing.T) {
			reads, runs := 0, 0
			runtime := &passiveCheckRuntime{
				kubectl: framework.NewArgsScope(func(context.Context, ...string) (string, error) {
					return `{"items":[{"metadata":{"name":"worker-pod"},"spec":{"hostname":"worker-0"}}]}`, nil
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
					if strings.Contains(command, ".check_runner.hc_program.out") {
						reads++
						if reads == 1 {
							require.Zero(t, runs)
							return baseline, nil
						}
						require.Equal(t, 1, runs)
						return nvidiaCheckRunnerOutput(""), nil
					}
					require.Contains(t, command, "worker-0.nvidia_libraries.hc_program.out")
					return passingNVIDIALibrariesLog, nil
				}),
			}
			steps := PassiveChecks{
				info: &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"}, runtime: runtime,
				gpuWorker: framework.WorkerInfo{Name: "worker-0"},
			}
			require.NoError(t, steps.runFreshNVIDIAHealthCheckProgram(t.Context()))
			err := steps.nvidiaLibrariesHCProgramCheckPasses(t.Context())
			if baseline == nvidiaCheckRunnerOutput("") {
				require.ErrorContains(t, err, "runner output is unchanged")
			} else {
				require.NoError(t, err)
			}
			steps.CleanupAndReset(t.Context())
			assert.Empty(t, steps.nvidiaHCProgramBaseline)
		})
	}
}

func TestPassiveGPUJobCancelsOwnedJobAfterCancelledWait(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var cancelled bool
	runtime := &passiveCheckRuntime{jail: framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		switch {
		case strings.Contains(command, "sbatch "):
			assert.Contains(t, command, "--nodelist='worker-0'")
			return "42\n", nil
		case command == "scancel '42'":
			require.NoError(t, ctx.Err(), "cleanup must survive scenario cancellation")
			cancelled = true
		case strings.HasPrefix(command, "squeue "):
			if !cancelled {
				cancel()
				return "PENDING\n", nil
			}
		}
		return "", nil
	})}
	steps := PassiveChecks{
		runtime: runtime, slurm: framework.NewSlurmClient(runtime),
		gpuWorker: framework.WorkerInfo{Name: "worker-0"},
	}
	require.Error(t, steps.aSmallGPUSlurmJobRunsOnTheSelectedGPUWorker(ctx))
	assert.True(t, cancelled)
}
