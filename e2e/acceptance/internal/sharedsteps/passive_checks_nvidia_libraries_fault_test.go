package sharedsteps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	libcudaTarget            = "/usr/lib/x86_64-linux-gnu/libcuda.so.580.173.02"
	drainedByNVIDIALibraries = "NodeName=worker-0 State=MIXED+DRAIN CPUAlloc=1\n   Reason=[software_problem] nvidia_libraries: libcuda.so.1: file too short [prolog] [root@2026-11-17T13:00:00]\n"
)

// withWorkerPod lets the gpu failure runtime answer slurmd-container commands and worker pod lookups.
func withWorkerPod(runtime *recordingRuntime, handle func(command string) (string, error)) *recordingRuntime {
	runtime.kubectl = framework.NewArgsScope(func(context.Context, ...string) (string, error) {
		return `{"items":[{"metadata":{"name":"worker-0"},"spec":{"hostname":"worker-0","nodeName":"host-0"}}]}`, nil
	})
	runtime.workerPod = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		runtime.log = append(runtime.log, "pod: "+command)
		return handle(command)
	})
	return runtime
}

func libcudaFault() nvidiaLibraryFault {
	return nvidiaLibraryFault{
		mounted:  true,
		worker:   framework.WorkerInfo{Name: "worker-0"},
		pod:      framework.WorkerPodInfo{SlurmNodeName: "worker-0", PodName: "worker-0"},
		soname:   "libcuda.so.1",
		target:   libcudaTarget,
		fakePath: "/run/soperator-e2e/libcuda.so.580.173.02",
	}
}

func TestValidateNVIDIALibraryTarget(t *testing.T) {
	target, err := validateNVIDIALibraryTarget("libcuda.so.1", libcudaTarget+"\n")
	require.NoError(t, err)
	assert.Equal(t, libcudaTarget, target)
	_, err = validateNVIDIALibraryTarget("libnvidia-ml.so.1", "/usr/lib/aarch64-linux-gnu/libnvidia-ml.so.580.173.02")
	require.NoError(t, err)

	for name, resolved := range map[string]string{
		"other library":    "/usr/lib/x86_64-linux-gnu/libnvidia-ml.so.580.173.02",
		"outside lib dir":  "/opt/libcuda.so.580.173.02",
		"unversioned":      "/usr/lib/x86_64-linux-gnu/libcuda.so",
		"dangling symlink": "",
		"path traversal":   "/usr/lib/x86_64-linux-gnu/../../../etc/libcuda.so.1",
		"two lines":        libcudaTarget + "\n/etc/passwd",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := validateNVIDIALibraryTarget("libcuda.so.1", resolved)
			require.Error(t, err)
		})
	}
}

func TestNVIDIALibraryFaultCommands(t *testing.T) {
	fault := libcudaFault()
	assert.Equal(t,
		`set -e; mkdir -p '/run/soperator-e2e'; printf '%s\n' 'soperator-e2e-nvidia-libraries-fault' > '/run/soperator-e2e/libcuda.so.580.173.02'; mount --bind '/run/soperator-e2e/libcuda.so.580.173.02' '/mnt/jail/usr/lib/x86_64-linux-gnu/libcuda.so.580.173.02'`,
		nvidiaLibraryFaultCommand(fault, false))
	assert.Contains(t, nvidiaLibraryFaultCommand(fault, true), "head -c 4096 /dev/zero >> '/run/soperator-e2e/libcuda.so.580.173.02'; mount --bind")

	assert.Equal(t,
		removeFakeLibraryCommand("/mnt/jail/usr/lib/x86_64-linux-gnu/libcuda.so.580.173.02", "/run/soperator-e2e/libcuda.so.580.173.02"),
		nvidiaLibraryFaultRemoveCommand(fault))

	assert.Equal(t, 37, fakeNVIDIALibrarySize(false))
	assert.Less(t, fakeNVIDIALibrarySize(false), nvidiaNonPlaceholderLibraryMinSize)
	assert.Greater(t, fakeNVIDIALibrarySize(true), nvidiaNonPlaceholderLibraryMinSize)
	assert.Equal(t, `chroot /mnt/jail readlink -f "/usr/lib/$(uname -m)-linux-gnu/"'libcuda.so.1'`, resolveNVIDIALibraryCommand("libcuda.so.1"))
	assert.Equal(t, "[software_problem] nvidia_libraries: libnvidia-ml.so.1: not loadable [prolog]", nvidiaLibrariesReason("libnvidia-ml.so.1", "not loadable", "prolog"))
}

func TestReplacingAnNVIDIALibraryMountsAVerifiedFake(t *testing.T) {
	for _, tt := range []struct {
		fault string
		size  string
	}{
		{fault: "a truncated file", size: "37"},
		{fault: "a corrupt file", size: "4133"},
	} {
		t.Run(tt.fault, func(t *testing.T) {
			runtime := withWorkerPod(newRecordingRuntime(t, nil), func(command string) (string, error) {
				switch {
				case strings.Contains(command, "readlink"):
					return libcudaTarget + "\n", nil
				case strings.Contains(command, "stat -L"):
					return tt.size + "\n", nil
				default:
					return "", nil
				}
			})
			steps := recordingPassiveChecks(runtime)
			require.NoError(t, steps.aLibraryIsReplacedWithAFaultyFileOnTheSelectedGPUWorkerOnly(t.Context(), "libcuda.so.1", tt.fault))
			require.NotNil(t, steps.nvidiaLibraryFault)
			assert.Equal(t, libcudaTarget, steps.nvidiaLibraryFault.target)
			assert.True(t, steps.nvidiaLibraryFault.mounted)
			assert.Len(t, commandsContaining(runtime.log, "mount --bind"), 1)
		})
	}
}

func TestReplacingAnNVIDIALibraryRecordsTheFaultEvenWhenVerificationFails(t *testing.T) {
	runtime := withWorkerPod(newRecordingRuntime(t, nil), func(command string) (string, error) {
		switch {
		case strings.Contains(command, "readlink"):
			return libcudaTarget, nil
		case strings.Contains(command, "stat -L"):
			return "96284520", nil
		default:
			return "", nil
		}
	})
	steps := recordingPassiveChecks(runtime)

	require.ErrorContains(t, steps.aLibraryIsReplacedWithAFaultyFileOnTheSelectedGPUWorkerOnly(t.Context(), "libcuda.so.1", "a truncated file"), "expected the 37-byte fake")
	require.NotNil(t, steps.nvidiaLibraryFault, "cleanup must still remove a fault that was mounted")
	assert.True(t, steps.nvidiaLibraryFault.mounted)
}

// nvidiaRecoveryRuntime answers the commands of fault removal, a fresh HealthCheckProgram run and the
// resume that follows it; libraryOK decides whether that run shows nvidia_libraries passing.
func nvidiaRecoveryRuntime(t *testing.T, drained string, libraryOK bool, podSize string) (*recordingRuntime, *bool) {
	t.Helper()
	resumed := false
	runner := freshHCProgramOutput
	if !libraryOK {
		runner = strings.Replace(freshHCProgramOutput, "Check nvidia_libraries: OK", "Check nvidia_libraries: FAIL (libcuda.so.1: file too short)", 1)
	}
	runtime := withWorkerPod(newRecordingRuntimeWithWorker(t, func(command string) (string, error) {
		switch {
		case strings.HasPrefix(command, "scontrol update"):
			resumed = true
			return "", nil
		case resumed:
			return "NodeName=worker-0 State=MIXED CPUAlloc=1\n", nil
		default:
			return drained, nil
		}
	}, nil, func(command string) (string, error) {
		switch {
		case command == "date -u +%s":
			return "1767225600\n", nil
		case strings.Contains(command, "worker-0.check_runner.hc_program.out"):
			return runner, nil
		case strings.Contains(command, "worker-0.nvidia_libraries.hc_program.out"):
			return nvidiaLibrariesPassingLog, nil
		default:
			return "", nil
		}
	}), func(command string) (string, error) {
		if strings.Contains(command, "stat -L") {
			return podSize + "\n", nil
		}
		return "", nil
	})
	return runtime, &resumed
}

func injectedLibcudaSteps(runtime *recordingRuntime) *PassiveChecks {
	steps := recordingPassiveChecks(runtime)
	fault := libcudaFault()
	steps.nvidiaLibraryFault = &fault
	return steps
}

func TestNVIDIALibraryCleanupRemovesTheFaultAndThenUndrains(t *testing.T) {
	runtime, resumed := nvidiaRecoveryRuntime(t, drainedByNVIDIALibraries, true, "96284520")
	steps := injectedLibcudaSteps(runtime)
	steps.nvidiaFaultJob = framework.SbatchJob{ID: "43"}

	ctx := framework.WithWorkerCleanup(t.Context())
	steps.cleanupAndResetNVIDIALibraryFault(ctx)
	require.NoError(t, framework.FinishWorkerReservation(ctx, nil))

	var order []string
	for _, entry := range runtime.log {
		switch {
		case strings.Contains(entry, "scancel '43'"):
			order = append(order, "cancel job")
		case strings.Contains(entry, "umount"):
			order = append(order, "remove fault")
		case strings.Contains(entry, "hc_program.sh"):
			order = append(order, "hc_program")
		case strings.Contains(entry, "check_runner.hc_program.out"):
			order = append(order, "read fresh result")
		case strings.Contains(entry, "state=resume"):
			order = append(order, "resume")
		}
	}
	assert.Equal(t, []string{"cancel job", "remove fault", "hc_program", "read fresh result", "resume"}, order)
	assert.True(t, *resumed)
	assert.Nil(t, steps.nvidiaLibraryFault)
}

func TestNVIDIALibraryCleanupLeavesADrainItDidNotCause(t *testing.T) {
	for name, drained := range map[string]string{
		"no fault injected": drainedByNVIDIALibraries,
		"other library":     strings.Replace(drainedByNVIDIALibraries, "libcuda.so.1", "libnvidia-ml.so.1", 1),
	} {
		t.Run(name, func(t *testing.T) {
			runtime, resumed := nvidiaRecoveryRuntime(t, drained, true, "96284520")
			steps := injectedLibcudaSteps(runtime)
			if name == "no fault injected" {
				steps.nvidiaLibraryFault = nil
			}

			steps.cleanupAndResetNVIDIALibraryFault(t.Context())

			assert.False(t, *resumed)
			assert.Empty(t, commandsContaining(runtime.log, "hc_program.sh"))
		})
	}
}

func TestNVIDIALibraryCleanupDoesNotResumeWhileTheCheckStillFails(t *testing.T) {
	runtime, resumed := nvidiaRecoveryRuntime(t, drainedByNVIDIALibraries, false, "96284520")
	steps := injectedLibcudaSteps(runtime)

	ctx := framework.WithWorkerCleanup(t.Context())
	steps.cleanupAndResetNVIDIALibraryFault(ctx)

	assert.False(t, *resumed)
	require.ErrorContains(t, framework.FinishWorkerReservation(ctx, nil), "Check nvidia_libraries: FAIL (libcuda.so.1: file too short)")
	assert.Nil(t, steps.nvidiaLibraryFault)
}

func TestNVIDIALibraryCleanupReportsAFaultItCannotRemove(t *testing.T) {
	for _, size := range []string{"37", "4133", "unreadable"} {
		t.Run(size, func(t *testing.T) {
			runtime, resumed := nvidiaRecoveryRuntime(t, drainedByNVIDIALibraries, true, size)
			steps := injectedLibcudaSteps(runtime)

			ctx := framework.WithWorkerCleanup(t.Context())
			steps.cleanupAndResetNVIDIALibraryFault(ctx)

			err := framework.FinishWorkerReservation(ctx, nil)
			require.ErrorContains(t, err, "expected the real library")
			require.ErrorContains(t, err, "kubectl exec -n soperator worker-0 -c slurmd -- umount /mnt/jail"+libcudaTarget)
			assert.Greater(t, len(commandsContaining(runtime.log, "umount")), 1, "removal is retried within the hook")
			assert.False(t, *resumed, "a worker whose library is still broken must not be resumed")
			assert.Nil(t, steps.nvidiaLibraryFault, "the step family is recreated for the next scenario anyway")
		})
	}
}

// TestRemoveFakeLibraryCommand runs the generated script with a stub umount over temporary files.
func TestRemoveFakeLibraryCommand(t *testing.T) {
	realLibraryContent := "\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00 real driver library content"
	for _, tt := range []struct {
		name           string
		initial        string // "fake", "real" or "" for an unreadable target
		umountRestores bool
		wantOK         bool
		wantUmounts    int
	}{
		{name: "fake on top", initial: "fake", umountRestores: true, wantOK: true, wantUmounts: 1},
		{name: "already restored", initial: "real", wantOK: true},
		{name: "unreadable target", initial: "", wantOK: false},
		{name: "fake stays mounted", initial: "fake", wantOK: false, wantUmounts: 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			target, fake, realLibrary, calls := dir+"/libcuda.so.580", dir+"/fake", dir+"/real", dir+"/umounts"
			require.NoError(t, os.WriteFile(fake, []byte(nvidiaLibraryFaultMarker+"\n"), 0o644))
			require.NoError(t, os.WriteFile(realLibrary, []byte(realLibraryContent), 0o644))
			switch tt.initial {
			case "fake":
				require.NoError(t, os.WriteFile(target, []byte(nvidiaLibraryFaultMarker+"\n"), 0o644))
			case "real":
				require.NoError(t, os.WriteFile(target, []byte(realLibraryContent), 0o644))
			}
			stub := fmt.Sprintf(`umount() { [ "$1" = "--" ] || return 1; echo x >> %s; %s; }; `,
				framework.ShellQuote(calls), map[bool]string{true: "cp " + framework.ShellQuote(realLibrary) + ` "$2"`, false: "true"}[tt.umountRestores])

			err := exec.CommandContext(t.Context(), "bash", "-c", stub+removeFakeLibraryCommand(target, fake)).Run()

			if tt.wantOK {
				require.NoError(t, err)
				assert.NoFileExists(t, fake)
			} else {
				require.Error(t, err)
				assert.FileExists(t, fake, "the backing file must stay while the fake may still be mounted")
			}
			umounts, _ := os.ReadFile(calls)
			assert.Equal(t, tt.wantUmounts, strings.Count(string(umounts), "x"))
		})
	}
}

func TestCPUOnlyJobIsRequeuedWithoutRunning(t *testing.T) {
	cancelled := false
	runtime := newRecordingRuntime(t, func(command string) (string, error) {
		switch {
		case strings.HasPrefix(command, "squeue"):
			if cancelled {
				return "", nil
			}
			return "PENDING\n", nil
		case strings.HasPrefix(command, "sacct"):
			return "42|PENDING|0:0|BeginTime|Unknown|Unknown\n", nil
		case strings.HasPrefix(command, "scontrol -o show job"):
			return "JobId=42 JobName=e2e-passive-nvidia-libraries-fault JobState=PENDING Restarts=1\n", nil
		case strings.HasPrefix(command, "scancel"):
			cancelled = true
			return "", nil
		default:
			t.Fatalf("unexpected jail command: %s", command)
			return "", nil
		}
	})
	steps := recordingPassiveChecks(runtime)
	steps.nvidiaFaultJob = framework.SbatchJob{ID: "42"}

	require.NoError(t, steps.theCPUOnlyJobIsRequeuedWithoutRunning(t.Context()))
	assert.True(t, cancelled)
	assert.True(t, steps.nvidiaFaultJob.IsZero())
}

func TestCPUOnlyJobThatCompletesFailsTheRequeueStep(t *testing.T) {
	runtime := newRecordingRuntime(t, func(command string) (string, error) {
		switch {
		case strings.HasPrefix(command, "squeue"):
			return "", nil
		case strings.HasPrefix(command, "sacct"):
			return "42|COMPLETED|0:0|None|2026-11-17T13:00:00|2026-11-17T13:00:01\n", nil
		case strings.HasPrefix(command, "scontrol -o show job"):
			return "JobId=42 JobState=COMPLETED Restarts=0\n", nil
		default:
			return "", nil
		}
	})
	steps := recordingPassiveChecks(runtime)
	steps.nvidiaFaultJob = framework.SbatchJob{ID: "42"}

	require.ErrorContains(t, steps.theCPUOnlyJobIsRequeuedWithoutRunning(t.Context()), "completed successfully although the Prolog check should have failed")
}

func runnerOutputStartedAt(started string, finished bool, failedCheck string) string {
	output := "[" + started + " UTC] INFO: Started\n"
	if failedCheck != "" {
		return output + "[" + started + " UTC] INFO: Check " + failedCheck + ": FAIL (broken)\n"
	}
	if finished {
		output += "[" + started + " UTC] INFO: Check nvidia_libraries: OK\n[" + started + " UTC] INFO: Finished in 5.000 seconds\n"
	}
	return output
}

// periodicSyncRuntime serves worker clock readings and check_runner outputs in order, repeating the last one.
func periodicSyncRuntime(t *testing.T, clocks, outputs []string) *recordingRuntime {
	t.Helper()
	clockReads, outputReads := 0, 0
	return newRecordingRuntimeWithWorker(t, func(command string) (string, error) {
		require.Equal(t, "scontrol show config", command)
		return "HealthCheckInterval     = 120 sec\nHealthCheckProgram      = /opt/slurm_scripts/hc_program.sh\n", nil
	}, nil, func(command string) (string, error) {
		switch {
		case command == "date -u +%s":
			clock := clocks[min(clockReads, len(clocks)-1)]
			clockReads++
			return clock + "\n", nil
		case strings.Contains(command, "worker-0.check_runner.hc_program.out"):
			output := outputs[min(outputReads, len(outputs)-1)]
			outputReads++
			return output, nil
		default:
			t.Fatalf("unexpected worker command: %s", command)
			return "", nil
		}
	})
}

func TestPeriodicSyncInjectsOnlyWithEnoughTimeBeforeTheNextRun(t *testing.T) {
	// 1767225600 is 2026-01-01T00:00:00Z.
	t.Run("enough time after the first run", func(t *testing.T) {
		runtime := periodicSyncRuntime(t, []string{"1767225600", "1767225610"}, []string{
			runnerOutputStartedAt("2025-12-31 23:59:00.000", true, "nvidia_libraries"),
			runnerOutputStartedAt("2026-01-01 00:00:05.000", true, ""),
		})
		require.NoError(t, recordingPassiveChecks(runtime).thePeriodicHealthCheckProgramHasJustCompletedOnTheSelectedGPUWorker(t.Context()))
	})
	t.Run("waits for the following run when too little time is left", func(t *testing.T) {
		runtime := periodicSyncRuntime(t, []string{"1767225600", "1767225690", "1767225730"}, []string{
			runnerOutputStartedAt("2026-01-01 00:00:01.000", true, ""),
			runnerOutputStartedAt("2026-01-01 00:00:01.000", true, ""),
			runnerOutputStartedAt("2026-01-01 00:02:01.000", true, ""),
		})
		require.NoError(t, recordingPassiveChecks(runtime).thePeriodicHealthCheckProgramHasJustCompletedOnTheSelectedGPUWorker(t.Context()))
		assert.Len(t, commandsContaining(runtime.log, "date -u +%s"), 3)
	})
	t.Run("fails when no run leaves enough time", func(t *testing.T) {
		// The second run starts at 00:01:51 and is observed at 00:03:10, leaving 41 s.
		runtime := periodicSyncRuntime(t, []string{"1767225600", "1767225690", "1767225790"}, []string{
			runnerOutputStartedAt("2026-01-01 00:00:01.000", true, ""),
			runnerOutputStartedAt("2026-01-01 00:00:01.000", true, ""),
			runnerOutputStartedAt("2026-01-01 00:01:51.000", true, ""),
		})
		require.ErrorContains(t, recordingPassiveChecks(runtime).thePeriodicHealthCheckProgramHasJustCompletedOnTheSelectedGPUWorker(t.Context()),
			"no periodic HealthCheckProgram run on worker-0 left 1m0s before the next one")
	})
	t.Run("a fresh failed run is reported instead of timing out", func(t *testing.T) {
		runtime := periodicSyncRuntime(t, []string{"1767225600"}, []string{
			runnerOutputStartedAt("2026-01-01 00:00:05.000", false, "gpu_health_check"),
		})
		require.ErrorContains(t, recordingPassiveChecks(runtime).thePeriodicHealthCheckProgramHasJustCompletedOnTheSelectedGPUWorker(t.Context()),
			"failed: [2026-01-01 00:00:05.000 UTC] INFO: Check gpu_health_check: FAIL (broken)")
	})
}
