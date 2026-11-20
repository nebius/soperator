package sharedsteps

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	periodicHealthCheckContext      = "hc_program"
	periodicHealthCheckBaselineWait = 30 * time.Second
	healthCheckerVersionFact        = "health_checker_version"

	// gpuWorkerFactsScript prints, as meta_field=value lines, what the health-checker should have detected
	// on the worker. It runs in a login shell, as gpu_health_check does, so the jail's CUDA PATH applies.
	// Per-GPU values are taken from the first GPU, as the health-checker reports them.
	// mlnx_version is the loaded host driver, which can differ from the OFED packages in the jail.
	gpuWorkerFactsScript = `echo "kernel_version=$(uname -r)"
echo "gpu_driver_version=$(timeout 30s nvidia-smi --query-gpu=driver_version --format=csv,noheader | head -n 1)"
echo "gpu_bios_version=$(timeout 30s nvidia-smi --query-gpu=vbios_version --format=csv,noheader | head -n 1)"
echo "cuda_version=$(nvcc --version | sed -n 's/^Build //p')"
echo "dcgmi_version=$(timeout 30s dcgmi --version | sed -n 's/.*version: *//p')"
echo "lib_nccl_version=$(dpkg-query -W -f='${Version}' libnccl2)"
echo "mlnx_version=$(cat /sys/module/mlx5_core/version)"
echo "health_checker_version=$(health-checker --version | sed -n 's/^health-checker version //p')"`
)

var (
	checkRunnerPlatformTagPattern = regexp.MustCompile(`Environment CHECKS_PLATFORM_TAG="([^"]*)"`)

	// Report metadata compared with gpuWorkerFactsScript output, and where the expected value comes from.
	gpuHealthCheckMetaSources = []struct{ field, source string }{
		{field: "kernel_version", source: "uname -r"},
		{field: "gpu_driver_version", source: "nvidia-smi driver_version of the first GPU"},
		{field: "gpu_bios_version", source: "nvidia-smi vbios_version of the first GPU"},
		{field: "cuda_version", source: "nvcc --version"},
		{field: "dcgmi_version", source: "dcgmi --version"},
		{field: "lib_nccl_version", source: "libnccl2 package"},
		{field: "mlnx_version", source: "/sys/module/mlx5_core/version"},
	}
)

type periodicGPUHealthCheck struct {
	worker framework.WorkerInfo
	// Worker clock and the latest report's run ID at selection. Both clocks have whole-second precision,
	// so a fresh report must have a newer-or-equal timestamp and a different run ID.
	notBefore     time.Time
	baselineRunID string
	report        healthCheckReport
}

func (s *PassiveChecks) thePoweredUpGPUWorkersAreSelectedForPeriodicHealthChecks(ctx context.Context) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	if err := snapshot.RequireGPUWorkersUsable(); framework.IsInsufficientWorkers(err) {
		s.runtime.Logf("passive checks: no GPU workers configured for periodic health checks, skipping scenario")
		return godog.ErrSkip
	} else if err != nil {
		return fmt.Errorf("validate GPU workers for periodic health checks: %w", err)
	}

	s.periodicGPUChecks = nil
	for _, worker := range snapshot.GPUWorkers {
		if !worker.SlurmNode.IsReadyForImmediateWork() {
			s.runtime.Logf("passive checks: Slurm does not run HealthCheckProgram on GPU worker %s in state %s, not checking it", worker.Name, worker.SlurmNode.State)
			continue
		}
		notBefore, err := workerClockNow(ctx, s.runtime, worker)
		if err != nil {
			return err
		}
		baselineRunID := s.periodicGPUHealthCheckBaselineRunID(ctx, worker)
		s.periodicGPUChecks = append(s.periodicGPUChecks, periodicGPUHealthCheck{worker: worker, notBefore: notBefore, baselineRunID: baselineRunID})
	}
	if len(s.periodicGPUChecks) == 0 {
		s.runtime.Logf("passive checks: all GPU workers are powered down, skipping scenario")
		return godog.ErrSkip
	}
	s.runtime.Logf("passive checks: selected GPU workers for periodic health checks: %v", s.periodicGPUWorkerNames())
	return nil
}

func (s *PassiveChecks) aNewPeriodicGPUHealthCheckCompletesOnEverySelectedGPUWorker(ctx context.Context) error {
	if len(s.periodicGPUChecks) == 0 {
		return fmt.Errorf("GPU workers for periodic health checks are not selected")
	}
	configuration, err := s.slurm.Configuration(ctx)
	if err != nil {
		return err
	}
	timeout, err := periodicHealthCheckWait(configuration)
	if err != nil {
		return err
	}
	s.runtime.Logf("passive checks: waiting up to %s for periodic GPU health checks on %v", timeout, s.periodicGPUWorkerNames())
	// Workers run their checks independently, so waiting for them one after another within one deadline
	// costs no extra time.
	deadline := time.Now().Add(timeout)
	for i := range s.periodicGPUChecks {
		check := &s.periodicGPUChecks[i]
		report, err := s.waitForPeriodicGPUHealthCheckReport(ctx, *check, max(time.Until(deadline), framework.DefaultPollInterval))
		if err != nil {
			return err
		}
		check.report = report
	}
	return nil
}

func (s *PassiveChecks) everyNewPeriodicGPUHealthCheckReportIsPassing(context.Context) error {
	return s.checkEveryPeriodicGPUHealthCheck(func(check periodicGPUHealthCheck) error {
		if _, err := assertHealthCheckReportsPassing([]healthCheckReport{check.report}); err != nil {
			return fmt.Errorf("%w; non-passing checks: %s", err, healthCheckReportProblems(check.report))
		}
		return nil
	})
}

func (s *PassiveChecks) everyNewPeriodicGPUHealthCheckReportHasMetadataMatchingItsWorker(ctx context.Context) error {
	return s.checkEveryPeriodicGPUHealthCheck(func(check periodicGPUHealthCheck) error {
		facts, err := s.gpuWorkerFacts(ctx, check.worker)
		if err != nil {
			return err
		}
		s.runtime.Logf("passive checks: %s runs health-checker %s; report run %s metadata: %s",
			check.worker.Name, facts[healthCheckerVersionFact], healthCheckRunID(check.report), formatGPUHealthCheckMeta(check.report))
		return assertGPUHealthCheckMeta(check.report, check.worker.Name, facts)
	})
}

func (s *PassiveChecks) everyNewPeriodicGPUHealthCheckReportHasItsRawCommandOutputs(ctx context.Context) error {
	outputDir := healthCheckerStdoutOutputDir(s.info.TargetSoperatorVersion)
	return s.checkEveryPeriodicGPUHealthCheck(func(check periodicGPUHealthCheck) error {
		return assertHealthCheckReportRawOutputsPresent(ctx, s.runtime, check.worker, outputDir, check.report)
	})
}

func (s *PassiveChecks) checkEveryPeriodicGPUHealthCheck(check func(periodicGPUHealthCheck) error) error {
	if len(s.periodicGPUChecks) == 0 {
		return fmt.Errorf("periodic GPU health-check reports are not captured")
	}
	var problems []error
	for _, periodic := range s.periodicGPUChecks {
		if periodic.report.Status == "" {
			problems = append(problems, fmt.Errorf("%s: periodic GPU health-check report is not captured", periodic.worker.Name))
			continue
		}
		if err := check(periodic); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", periodic.worker.Name, err))
		}
	}
	return errors.Join(problems...)
}

// periodicGPUHealthCheckBaselineRunID returns the run ID of the report present at selection, or "" without
// one. A report caught while being rewritten is read again.
func (s *PassiveChecks) periodicGPUHealthCheckBaselineRunID(ctx context.Context, worker framework.WorkerInfo) string {
	targetPath := gpuHealthCheckOutputPath(s.info.TargetSoperatorVersion, worker.Name, periodicHealthCheckContext)
	var runID string
	err := s.runtime.WaitFor(ctx, fmt.Sprintf("complete GPU health-check report %s", targetPath), periodicHealthCheckBaselineWait, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		out, exists, err := readWorkerFileIfExists(waitCtx, s.runtime, worker, targetPath)
		if err != nil || !exists {
			return !exists, err
		}
		if reports, parseErr := parseHealthCheckReports(out); parseErr == nil {
			runID = healthCheckRunID(reports[len(reports)-1])
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		// Without a complete report there is nothing a later report could be confused with.
		s.runtime.Logf("passive checks: no complete GPU health-check report on %s at selection: %v", worker.Name, err)
		return ""
	}
	return runID
}

// waitForPeriodicGPUHealthCheckReport waits for the hc_program gpu_health_check report of a run that Slurm
// started on its own after selection. The report file is rewritten on every run.
func (s *PassiveChecks) waitForPeriodicGPUHealthCheckReport(ctx context.Context, check periodicGPUHealthCheck, timeout time.Duration) (healthCheckReport, error) {
	worker := check.worker
	targetPath := gpuHealthCheckOutputPath(s.info.TargetSoperatorVersion, worker.Name, periodicHealthCheckContext)
	var content string
	var fresh healthCheckReport
	description := fmt.Sprintf("periodic GPU health-check report %s since %s other than run %q", targetPath, check.notBefore.Format(time.RFC3339), check.baselineRunID)
	err := s.runtime.WaitFor(ctx, description, timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		out, exists, err := readWorkerFileIfExists(waitCtx, s.runtime, worker, targetPath)
		if err != nil || !exists {
			return false, err
		}
		content = out
		if reports, parseErr := parseHealthCheckReports(out); parseErr == nil {
			report := reports[len(reports)-1]
			isFresh, err := isFreshPeriodicGPUHealthCheckReport(report, check)
			if err != nil || !isFresh {
				return false, err
			}
			fresh = report
			return true, nil
		}
		// The report line may still be being written, or the run produced no report.
		return false, nil
	})
	if err == nil {
		return fresh, nil
	}
	err = fmt.Errorf("wait for a periodic %s report on %s (CHECKS_PLATFORM_TAG=%s): %w",
		gpuHealthCheckName, worker.Name, s.checkRunnerPlatformTag(ctx, worker), err)
	if strings.TrimSpace(content) != "" {
		return healthCheckReport{}, fmt.Errorf("%w; latest content:\n%s", err, strings.TrimSpace(content))
	}
	return healthCheckReport{}, err
}

func isFreshPeriodicGPUHealthCheckReport(report healthCheckReport, check periodicGPUHealthCheck) (bool, error) {
	reportedAt, err := healthCheckReportTimestamp(report)
	if err != nil {
		return false, err
	}
	runID := healthCheckRunID(report)
	return runID != "" && runID != check.baselineRunID && !reportedAt.Before(check.notBefore), nil
}

// checkRunnerPlatformTag explains a missing report: gpu_health_check only runs on the platforms in its config.
func (s *PassiveChecks) checkRunnerPlatformTag(ctx context.Context, worker framework.WorkerInfo) string {
	out, exists, err := readWorkerFileIfExists(ctx, s.runtime, worker, checkRunnerOutputPath(s.info.TargetSoperatorVersion, worker.Name, periodicHealthCheckContext))
	if err != nil || !exists {
		return "unknown"
	}
	if match := checkRunnerPlatformTagPattern.FindStringSubmatch(out); len(match) == 2 {
		return match[1]
	}
	return "unknown"
}

// gpuWorkerFacts returns gpuWorkerFactsScript values by meta field name.
func (s *PassiveChecks) gpuWorkerFacts(ctx context.Context, worker framework.WorkerInfo) (map[string]string, error) {
	out, err := s.runtime.Worker(worker).RunWithDefaultRetry(ctx, framework.ShellJoin("bash", "-lc", gpuWorkerFactsScript))
	if err != nil {
		return nil, fmt.Errorf("read GPU software versions: %w", err)
	}
	return parseKeyValues(out), nil
}

func (s *PassiveChecks) periodicGPUWorkerNames() []string {
	names := make([]string, 0, len(s.periodicGPUChecks))
	for _, check := range s.periodicGPUChecks {
		names = append(names, check.worker.Name)
	}
	return names
}

// assertGPUHealthCheckMeta checks that the health-checker detected the worker it ran on.
func assertGPUHealthCheckMeta(report healthCheckReport, workerName string, facts map[string]string) error {
	var problems []error
	if healthCheckRunID(report) == "" {
		problems = append(problems, fmt.Errorf("meta.run_id is empty"))
	}
	if _, err := healthCheckReportTimestamp(report); err != nil {
		problems = append(problems, err)
	}
	environment, _ := report.Meta["environment"].(string)
	if !environmentHasKeyValue(environment, "SLURMD_NODENAME", workerName) {
		problems = append(problems, fmt.Errorf("meta.environment does not contain SLURMD_NODENAME=%s: %q", workerName, environment))
	}
	for _, expected := range gpuHealthCheckMetaSources {
		want := facts[expected.field]
		if want == "" {
			problems = append(problems, fmt.Errorf("read %s for meta.%s on the worker: no value", expected.source, expected.field))
		} else if got := healthCheckMetaString(report, expected.field); got != want {
			problems = append(problems, fmt.Errorf("meta.%s is %q, %s on the worker reports %q", expected.field, got, expected.source, want))
		}
	}
	return errors.Join(problems...)
}

func formatGPUHealthCheckMeta(report healthCheckReport) string {
	fields := make([]string, 0, len(gpuHealthCheckMetaSources))
	for _, expected := range gpuHealthCheckMetaSources {
		fields = append(fields, fmt.Sprintf("%s=%s", expected.field, healthCheckMetaString(report, expected.field)))
	}
	return strings.Join(fields, " ")
}
