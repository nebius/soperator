package sharedsteps

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	nvidiaLibrariesPassingLog = `nvidia-container-cli listed 40 libraries, 12 belong to the compute and utility capabilities
Loaded libcuda.so.1
Loaded libnvidia-ml.so.1
All 14 NVIDIA libraries are loadable
`
	staleHCProgramOutput = `[2025-12-31 23:58:00.000 UTC] INFO: Started
[2025-12-31 23:58:01.000 UTC] INFO: Check nvidia_libraries: OK
[2025-12-31 23:58:02.000 UTC] INFO: Finished in 2.000 seconds
`
	freshHCProgramOutput = `[2026-01-01 00:00:01.250 UTC] INFO: Started
[2026-01-01 00:00:02.000 UTC] INFO: Running check nvidia_libraries (/usr/bin/python3 ./nvidia_libraries.py), logging to /opt/soperator-outputs/local/slurm_scripts/worker-0.nvidia_libraries.hc_program.out
[2026-01-01 00:00:03.000 UTC] INFO: Check nvidia_libraries: OK
[2026-01-01 00:00:04.000 UTC] INFO: Finished in 2.750 seconds
`
)

func TestNVIDIALibrariesHealthCheckProgramRequiresRunStartedAfterTheManualRun(t *testing.T) {
	runnerReads, runs := 0, 0
	runtime := &passiveCheckRuntime{
		kubectl: framework.NewArgsScope(func(context.Context, ...string) (string, error) {
			return `{"items":[{"metadata":{"name":"worker-pod-0"},"spec":{"hostname":"worker-0"}}]}`, nil
		}),
		workerPod: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
			require.Equal(t, "SLURMD_NODENAME='worker-0' /opt/slurm_scripts/hc_program.sh", command)
			runs++
			return "", nil
		}),
		worker: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
			switch {
			case command == "date -u +%s":
				require.Zero(t, runs, "read the worker clock before running HealthCheckProgram")
				return "1767225600\n", nil
			case strings.Contains(command, "/opt/soperator-outputs/local/slurm_scripts/worker-0.check_runner.hc_program.out"):
				runnerReads++
				switch runnerReads {
				case 1:
					return staleHCProgramOutput, nil
				case 2:
					return "[2026-01-01 00:00:01.250 UTC] INFO: Started\n", nil
				default:
					return freshHCProgramOutput, nil
				}
			case strings.Contains(command, "/opt/soperator-outputs/local/slurm_scripts/worker-0.nvidia_libraries.hc_program.out"):
				return nvidiaLibrariesPassingLog, nil
			default:
				t.Fatalf("unexpected worker command: %s", command)
				return "", nil
			}
		}),
	}
	gpuWorker := framework.WorkerInfo{Name: "worker-0"}
	steps := PassiveChecks{
		info:      &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"},
		runtime:   runtime,
		worker:    gpuWorker,
		gpuWorker: gpuWorker,
	}

	require.NoError(t, steps.healthCheckProgramRunsOnTheSelectedWorker(t.Context()))
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), steps.hcProgramStartedAt)
	require.NoError(t, steps.theNVIDIALibrariesCheckPassesInHealthCheckProgramOnTheSelectedGPUWorker(t.Context()))
	assert.Equal(t, 1, runs)
	assert.Equal(t, 3, runnerReads)
}

func TestNVIDIALibrariesHealthCheckProgramRejectsFailedOrMissingCheck(t *testing.T) {
	for _, tt := range []struct {
		name    string
		output  string
		wantErr string
	}{
		{
			name:    "failed check",
			output:  strings.Replace(freshHCProgramOutput, "Check nvidia_libraries: OK", "Check nvidia_libraries: FAIL (libcuda.so.1: file too short)", 1),
			wantErr: "failed: [2026-01-01 00:00:03.000 UTC] INFO: Check nvidia_libraries: FAIL (libcuda.so.1: file too short)",
		},
		{
			name:    "check did not run",
			output:  strings.Replace(freshHCProgramOutput, "Check nvidia_libraries: OK", "Check boot_disk_full: OK", 1),
			wantErr: `check "nvidia_libraries" did not complete with OK`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &passiveCheckRuntime{worker: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
				if strings.Contains(command, "check_runner.hc_program.out") {
					return tt.output, nil
				}
				return nvidiaLibrariesPassingLog, nil
			})}
			steps := PassiveChecks{
				info:               &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"},
				runtime:            runtime,
				gpuWorker:          framework.WorkerInfo{Name: "worker-0"},
				hcProgramStartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			}
			err := steps.theNVIDIALibrariesCheckPassesInHealthCheckProgramOnTheSelectedGPUWorker(t.Context())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestNVIDIALibrariesCPUJobPrologReadsPrologLog(t *testing.T) {
	var logCommands []string
	runtime := &passiveCheckRuntime{worker: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		logCommands = append(logCommands, command)
		return nvidiaLibrariesPassingLog, nil
	})}
	steps := PassiveChecks{
		info:            &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"},
		runtime:         runtime,
		worker:          framework.WorkerInfo{Name: "worker-0"},
		cpuPrologOutput: strings.ReplaceAll(freshHCProgramOutput, "hc_program", "prolog"),
	}
	require.NoError(t, steps.theNVIDIALibrariesCheckPassedInTheCPUJobProlog(t.Context()))
	require.Len(t, logCommands, 1)
	assert.Contains(t, logCommands[0], "/opt/soperator-outputs/local/slurm_scripts/worker-0.nvidia_libraries.prolog.out")

	steps.cpuPrologOutput = strings.Replace(steps.cpuPrologOutput, "Check nvidia_libraries: OK", "Check boot_disk_full: OK", 1)
	require.Error(t, steps.theNVIDIALibrariesCheckPassedInTheCPUJobProlog(t.Context()))
}

func TestAssertNVIDIALibrariesLoadedDriverLibraries(t *testing.T) {
	require.NoError(t, assertNVIDIALibrariesLoadedDriverLibraries(nvidiaLibrariesPassingLog))

	for name, log := range map[string]string{
		"not a GPU worker": "Not a GPU worker, skipping\n",
		"driver library list skipped": `nvidia-container-cli exited with 1, skipping the driver library file check:
error
Loaded libcuda.so.1
Loaded libnvidia-ml.so.1
All 2 NVIDIA libraries are loadable
`,
		"unhandled exception": "Unhandled exception, skipping the check\nTraceback ...\n",
		"empty":               "",
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, assertNVIDIALibrariesLoadedDriverLibraries(log))
		})
	}
}

func TestCheckRunnerStartedAt(t *testing.T) {
	startedAt, err := checkRunnerStartedAt(freshHCProgramOutput)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 1, 250_000_000, time.UTC), startedAt)

	_, err = checkRunnerStartedAt("INFO: Started\n")
	require.Error(t, err)
}

func TestNVIDIALibrariesLogFinishedPattern(t *testing.T) {
	for _, finished := range []string{
		nvidiaLibrariesPassingLog,
		"Not a GPU worker, skipping\n",
		"Unhandled exception, skipping the check\nTraceback (most recent call last):\n",
		"Loaded libcuda.so.1\n/usr/lib/x86_64-linux-gnu/libnvidia-ml.so.1 is missing\n",
		"/usr/lib/x86_64-linux-gnu/libcuda.so.580.173.02 is 37 bytes, expected a real library\n",
		"Load libnvidia-ml.so.1: libnvidia-ml.so.1: invalid ELF header\n",
	} {
		assert.True(t, nvidiaLibrariesLogFinishedPattern.MatchString(finished), finished)
	}
	for _, running := range []string{
		"",
		"Loaded libcuda.so.1\n",
		"nvidia-container-cli exited with 1, skipping the driver library file check:\nerror\n",
	} {
		assert.False(t, nvidiaLibrariesLogFinishedPattern.MatchString(running), running)
	}
}

func TestNVIDIALibrariesCheckWaitsForAFinishedLog(t *testing.T) {
	reads := 0
	runtime := &passiveCheckRuntime{worker: framework.NewCommandScope(func(context.Context, string) (string, error) {
		reads++
		if reads == 1 {
			return "", nil
		}
		return nvidiaLibrariesPassingLog, nil
	})}
	steps := PassiveChecks{
		info:            &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"},
		runtime:         runtime,
		worker:          framework.WorkerInfo{Name: "worker-0"},
		cpuPrologOutput: strings.ReplaceAll(freshHCProgramOutput, "hc_program", "prolog"),
	}
	require.NoError(t, steps.theNVIDIALibrariesCheckPassedInTheCPUJobProlog(t.Context()))
	assert.Equal(t, 2, reads)
}
