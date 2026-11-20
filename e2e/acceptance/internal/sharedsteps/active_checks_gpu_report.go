package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

// activeGPUChecksHealthCheckerEnv is the health-checker env gpu-checks.sh runs on every worker.
const activeGPUChecksHealthCheckerEnv = "soperator_active_checks"

var (
	activeGPUCheckPlatformPattern = regexp.MustCompile(`(?m)^Platform found:\s*(\S+)\s*$`)
	healthCheckerPlatformPattern  = regexp.MustCompile(`^[1-9][0-9]*x[A-Z0-9]+$`)
)

type activeGPUCheckOutput struct {
	record activeCheckSlurmJobRecord
	output string
}

type activeGPUCheckReport struct {
	worker   framework.WorkerInfo
	platform string
	report   healthCheckReport
}

func (s *ActiveChecks) everyGPUActiveCheckReportPassedEveryActiveCheckTestForItsPlatform(ctx context.Context) error {
	return s.checkEveryGPUActiveCheckReport(func(report activeGPUCheckReport) error {
		out, err := s.runtime.Worker(report.worker).RunWithDefaultRetry(ctx, healthCheckerListCommand(activeGPUChecksHealthCheckerEnv, report.platform))
		if err != nil {
			return fmt.Errorf("list health-checker tests of %s on %s: %w", activeGPUChecksHealthCheckerEnv, report.platform, err)
		}
		expected := parseHealthCheckerTestList(out)
		if len(expected) == 0 {
			return fmt.Errorf("health-checker lists no %s tests for %s:\n%s", activeGPUChecksHealthCheckerEnv, report.platform, strings.TrimSpace(out))
		}
		return assertHealthCheckTestsPassed(report.report, expected)
	})
}

func (s *ActiveChecks) everyGPUActiveCheckReportHasItsRawCommandOutputs(ctx context.Context) error {
	outputDir := healthCheckerStdoutOutputDir(s.info.TargetSoperatorVersion)
	return s.checkEveryGPUActiveCheckReport(func(report activeGPUCheckReport) error {
		return assertHealthCheckReportRawOutputsPresent(ctx, s.runtime, report.worker, outputDir, report.report)
	})
}

func (s *ActiveChecks) checkEveryGPUActiveCheckReport(check func(activeGPUCheckReport) error) error {
	if len(s.gpuCheckOutputs) == 0 {
		return fmt.Errorf("GPU ActiveCheck outputs are not captured")
	}
	var problems []error
	for _, jobID := range slices.Sorted(maps.Keys(s.gpuCheckOutputs)) {
		captured := s.gpuCheckOutputs[jobID]
		worker := captured.record.NodeList
		report, err := parseActiveGPUCheckOutput(captured.output)
		if err == nil {
			report.worker = framework.WorkerInfo{Name: worker}
			err = check(report)
		}
		if err != nil {
			problems = append(problems, fmt.Errorf("%s (job %s): %w", worker, jobID, err))
		}
	}
	return errors.Join(problems...)
}

// parseActiveGPUCheckOutput reads the platform and the health-checker report gpu-checks.sh prints
// between "Health checker output:" and "Health checker status:".
func parseActiveGPUCheckOutput(output string) (activeGPUCheckReport, error) {
	match := activeGPUCheckPlatformPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return activeGPUCheckReport{}, fmt.Errorf("GPU ActiveCheck output does not name the platform:\n%s", strings.TrimSpace(output))
	}
	platform := match[1]
	if !healthCheckerPlatformPattern.MatchString(platform) {
		return activeGPUCheckReport{}, fmt.Errorf("GPU ActiveCheck output names an invalid platform %q", platform)
	}
	_, section, found := strings.Cut(output, "Health checker output:")
	if !found {
		return activeGPUCheckReport{}, fmt.Errorf("GPU ActiveCheck output has no health-checker output:\n%s", strings.TrimSpace(output))
	}
	section, _, _ = strings.Cut(section, "Health checker status:")
	report, err := parseHealthCheckReportSection(section)
	if err != nil {
		return activeGPUCheckReport{}, fmt.Errorf("parse GPU ActiveCheck health-checker report: %w", err)
	}
	return activeGPUCheckReport{platform: platform, report: report}, nil
}

// parseHealthCheckReportSection accepts the report as one JSON line, as json-partial prints it, or
// pretty-printed across lines.
func parseHealthCheckReportSection(section string) (healthCheckReport, error) {
	if reports, err := parseHealthCheckReports(section); err == nil {
		return reports[len(reports)-1], nil
	}
	start, end := strings.Index(section, "{"), strings.LastIndex(section, "}")
	if start < 0 || end < start {
		return healthCheckReport{}, fmt.Errorf("no JSON report in:\n%s", strings.TrimSpace(section))
	}
	var report healthCheckReport
	if err := json.Unmarshal([]byte(section[start:end+1]), &report); err != nil {
		return healthCheckReport{}, fmt.Errorf("decode JSON report: %w", err)
	}
	return report, nil
}

// healthCheckerListCommand runs in a login shell, as gpu-checks.sh runs health-checker. The env is a
// constant and the platform matched healthCheckerPlatformPattern, so neither needs quoting of its own.
func healthCheckerListCommand(env, platform string) string {
	return "bash -lc " + framework.ShellQuote(fmt.Sprintf("health-checker list -e %s -p %s", env, platform))
}

// parseHealthCheckerTestList reads `health-checker list` output: one "<test>: <description>" line per
// test, followed by its checks indented with a tab.
func parseHealthCheckerTestList(output string) []string {
	var tests []string
	for _, line := range strings.Split(output, "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		name, _, found := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !found || name == "" || strings.ContainsAny(name, " \t") || slices.Contains(tests, name) {
			continue
		}
		tests = append(tests, name)
	}
	return tests
}

// assertHealthCheckTestsPassed requires every expected test to have run and passed: a disabled or
// skipped test is reported as EMPTY, and a test the suite dropped is missing from the report.
func assertHealthCheckTestsPassed(report healthCheckReport, expected []string) error {
	var problems []string
	for _, test := range expected {
		status, found := report.StatusesByName[test]
		switch {
		case !found:
			problems = append(problems, test+": missing")
		case status != "PASS":
			problems = append(problems, fmt.Sprintf("%s: %s", test, status))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("run %s did not pass %d of %d expected tests: %s",
			healthCheckRunID(report), len(problems), len(expected), strings.Join(problems, ", "))
	}
	return nil
}
