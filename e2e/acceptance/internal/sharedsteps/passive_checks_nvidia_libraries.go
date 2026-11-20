package sharedsteps

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	nvidiaLibrariesCheckName = "nvidia_libraries"
	// Mirrors REQUIRED_SONAMES in nvidia_libraries.py. A run that loaded no more libraries than this
	// skipped the driver library list from nvidia-container-cli.
	nvidiaLibrariesRequiredSonameCount = 2
)

var (
	nvidiaLibrariesLoadedPattern = regexp.MustCompile(`(?m)^All (\d+) NVIDIA libraries are loadable\s*$`)
	// The last line nvidia_libraries.py logs on each way out: success, a skipped run, or the failed library.
	nvidiaLibrariesLogFinishedPattern = regexp.MustCompile(`(?m)^(All \d+ NVIDIA libraries are loadable|Not a GPU worker, skipping|Unhandled exception, skipping the check|\S+ is missing|\S+ is \d+ bytes, expected a real library|Load \S+: .*)\s*$`)
)

func (s *PassiveChecks) theNVIDIALibrariesCheckPassesInHealthCheckProgramOnTheSelectedGPUWorker(ctx context.Context) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	if s.hcProgramStartedAt.IsZero() {
		return fmt.Errorf("HealthCheckProgram run on %s is not captured", s.gpuWorker.Name)
	}
	output, err := s.waitForCheckRunnerOutputStartedSince(ctx, s.gpuWorker, "hc_program", s.hcProgramStartedAt, passiveHCProgramTimeout)
	if err != nil {
		return fmt.Errorf("hc_program check_runner output: %w", err)
	}
	if err := assertCheckRunnerHealthy(output); err != nil {
		return fmt.Errorf("hc_program on %s: %w", s.gpuWorker.Name, err)
	}
	return s.assertNVIDIALibrariesCheckPassed(ctx, s.gpuWorker, "hc_program", output)
}

func (s *PassiveChecks) theNVIDIALibrariesCheckPassedInTheCPUJobProlog(ctx context.Context) error {
	if s.cpuPrologOutput == "" {
		return fmt.Errorf("CPU job Prolog check_runner output is not captured")
	}
	return s.assertNVIDIALibrariesCheckPassed(ctx, s.worker, "prolog", s.cpuPrologOutput)
}

func (s *PassiveChecks) assertNVIDIALibrariesCheckPassed(ctx context.Context, worker framework.WorkerInfo, contextName, checkRunnerOutput string) error {
	if err := assertCheckRunnerCheckOK(checkRunnerOutput, nvidiaLibrariesCheckName); err != nil {
		return fmt.Errorf("%s on %s: %w", contextName, worker.Name, err)
	}
	logPath := checkLogPath(s.info.TargetSoperatorVersion, worker.Name, nvidiaLibrariesCheckName, contextName)
	log, err := waitForFinishedCheckLog(ctx, s.runtime, worker, logPath, nvidiaLibrariesLogFinishedPattern, passiveHCProgramTimeout)
	if err != nil {
		return fmt.Errorf("%s log: %w", nvidiaLibrariesCheckName, err)
	}
	if err := assertNVIDIALibrariesLoadedDriverLibraries(log); err != nil {
		return fmt.Errorf("%s %s log %s on %s: %w", nvidiaLibrariesCheckName, contextName, logPath, worker.Name, err)
	}
	return nil
}

// assertNVIDIALibrariesLoadedDriverLibraries rejects runs that exited 0 without checking the driver
// libraries, e.g. because the worker was not recognized as a GPU worker or nvidia-container-cli failed.
func assertNVIDIALibrariesLoadedDriverLibraries(log string) error {
	match := nvidiaLibrariesLoadedPattern.FindStringSubmatch(log)
	if len(match) != 2 {
		return fmt.Errorf("log does not report loaded NVIDIA libraries:\n%s", strings.TrimSpace(log))
	}
	loaded, err := strconv.Atoi(match[1])
	if err != nil {
		return fmt.Errorf("parse loaded NVIDIA library count %q: %w", match[1], err)
	}
	if loaded <= nvidiaLibrariesRequiredSonameCount {
		return fmt.Errorf("want more than the %d required sonames loaded, i.e. the nvidia-container-cli driver libraries too, got %d:\n%s",
			nvidiaLibrariesRequiredSonameCount, loaded, strings.TrimSpace(log))
	}
	return nil
}

// waitForCheckRunnerOutputStartedSince waits for a finished check_runner run that started at or after
// since, and fails with the failed check's line when that run failed. The output file is rewritten on
// every run, so a periodic run may replace the manual one; any run that started after the action is
// fresh enough.
func (s *PassiveChecks) waitForCheckRunnerOutputStartedSince(ctx context.Context, worker framework.WorkerInfo, contextName string, since time.Time, timeout time.Duration) (string, error) {
	targetPath := checkRunnerOutputPath(s.info.TargetSoperatorVersion, worker.Name, contextName)
	var content string
	err := s.runtime.WaitFor(ctx, fmt.Sprintf("check_runner output %s started since %s", targetPath, since.Format(time.RFC3339)), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		out, exists, err := readWorkerFileIfExists(waitCtx, s.runtime, worker, targetPath)
		if err != nil || !exists {
			return false, err
		}
		content = out
		startedAt, err := checkRunnerStartedAt(out)
		if err != nil {
			return false, err
		}
		if startedAt.Before(since) {
			return false, nil
		}
		// A failed check ends the run without "Finished in"; report it instead of waiting for the timeout.
		if failed := checkRunnerFailedCheck(out); failed != "" {
			return false, &framework.PermanentError{Err: fmt.Errorf("the run that started at %s failed: %s", startedAt.Format(time.RFC3339), failed)}
		}
		return strings.Contains(out, "INFO: Finished in"), nil
	})
	if err != nil {
		if strings.TrimSpace(content) != "" {
			return "", fmt.Errorf("%w; latest content:\n%s", err, strings.TrimSpace(content))
		}
		return "", err
	}
	return content, nil
}
