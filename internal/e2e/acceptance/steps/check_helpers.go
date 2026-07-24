package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	slurmScriptsOutputDir        = "/opt/soperator-outputs/slurm_scripts"
	healthCheckerStdoutOutputDir = "/opt/soperator-outputs/health_checker_cmd_stdout"
)

var (
	checkRunnerOKPattern      = regexp.MustCompile(`\bCheck ([^:\n]+): OK\b`)
	checkRunnerFailPattern    = regexp.MustCompile(`\bCheck ([^:\n]+): FAIL\b`)
	checkRunnerLogPathPattern = regexp.MustCompile(`logging to ((?:/mnt/jail)?/\S+)`)
	healthCheckConfigFields   = regexp.MustCompile(`(?m)^\s*(HealthCheckProgram|HealthCheckInterval)\s*=\s*(.+)$`)
)

type healthCheckReport struct {
	Status string         `json:"status"`
	Meta   map[string]any `json:"meta"`
}

func assertHealthCheckProgramConfigured(ctx context.Context, exec framework.Exec) error {
	out, err := exec.Jail().RunWithDefaultRetry(ctx, "scontrol show config | grep -E 'HealthCheckProgram|HealthCheckInterval'")
	if err != nil {
		return fmt.Errorf("read HealthCheckProgram Slurm config: %w", err)
	}
	values := map[string]string{}
	for _, match := range healthCheckConfigFields.FindAllStringSubmatch(out, -1) {
		if len(match) == 3 {
			values[match[1]] = strings.TrimSpace(match[2])
		}
	}
	program := values["HealthCheckProgram"]
	if program == "" || program == "(null)" || strings.EqualFold(program, "none") {
		return fmt.Errorf("HealthCheckProgram is not configured:\n%s", strings.TrimSpace(out))
	}
	interval := values["HealthCheckInterval"]
	if interval == "" || strings.HasPrefix(interval, "0") {
		return fmt.Errorf("HealthCheckInterval is not configured:\n%s", strings.TrimSpace(out))
	}
	return nil
}

func waitForHealthyCheckRunnerOutput(ctx context.Context, exec framework.Exec, targetPath string, timeout time.Duration) (string, error) {
	var content string
	err := exec.WaitFor(ctx, fmt.Sprintf("healthy check_runner output %s", targetPath), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		out, exists, err := readJailFileIfExists(waitCtx, exec, targetPath)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
		content = out
		if match := checkRunnerFailPattern.FindString(out); match != "" {
			return false, fmt.Errorf("check_runner has failed check %q:\n%s", match, strings.TrimSpace(out))
		}
		if strings.Contains(out, "ERROR:") {
			return false, fmt.Errorf("check_runner has errors:\n%s", strings.TrimSpace(out))
		}
		if !strings.Contains(out, "INFO: Finished in") {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		if strings.TrimSpace(content) != "" {
			return "", fmt.Errorf("%w; latest content:\n%s", err, strings.TrimSpace(content))
		}
		return "", err
	}
	if err := assertCheckRunnerHealthy(content); err != nil {
		return "", err
	}
	return content, nil
}

func waitForPassingHealthCheckReports(ctx context.Context, exec framework.Exec, targetPath string, timeout time.Duration) ([]string, error) {
	var content string
	var runIDs []string
	err := exec.WaitFor(ctx, fmt.Sprintf("passing health-check reports %s", targetPath), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		out, exists, err := readJailFileIfExists(waitCtx, exec, targetPath)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
		content = out
		reports, parseErr := parseHealthCheckReports(out)
		if parseErr == nil {
			ids, err := assertHealthCheckReportsPassing(reports)
			if err != nil {
				return false, err
			}
			runIDs = ids
			return true, nil
		}
		// The health-check output can be present before the JSON report line is complete.
		return false, nil
	})
	if err != nil {
		if strings.TrimSpace(content) != "" {
			return nil, fmt.Errorf("%w; latest content:\n%s", err, strings.TrimSpace(content))
		}
		return nil, err
	}
	return runIDs, nil
}

func readJailFileIfExists(ctx context.Context, exec framework.Exec, targetPath string) (string, bool, error) {
	quotedPath := framework.ShellQuote(targetPath)
	out, err := exec.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf("test -f %s && cat %s || true", quotedPath, quotedPath))
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(out) == "" {
		return "", false, nil
	}
	return out, true, nil
}

func removeJailFiles(ctx context.Context, exec framework.Exec, targetPaths ...string) error {
	if len(targetPaths) == 0 {
		return nil
	}
	var quoted []string
	for _, targetPath := range targetPaths {
		quoted = append(quoted, framework.ShellQuote(targetPath))
	}
	if _, err := exec.Jail().RunWithDefaultRetry(ctx, "rm -f "+strings.Join(quoted, " ")); err != nil {
		return fmt.Errorf("remove jail files %s: %w", strings.Join(targetPaths, ", "), err)
	}
	return nil
}

func checkRunnerOutputPath(worker, contextName string) string {
	return fmt.Sprintf("%s/%s.check_runner.%s.out", slurmScriptsOutputDir, worker, contextName)
}

func gpuHealthCheckOutputPath(worker, contextName string) string {
	return fmt.Sprintf("%s/%s.gpu_health_check.%s.out", slurmScriptsOutputDir, worker, contextName)
}

func assertCheckRunnerHealthy(output string) error {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return fmt.Errorf("check_runner output is empty")
	}
	if !strings.Contains(output, "INFO: Started") {
		return fmt.Errorf("check_runner output does not contain start marker:\n%s", trimmed)
	}
	if !strings.Contains(output, "INFO: Finished in") {
		return fmt.Errorf("check_runner output does not contain finish marker:\n%s", trimmed)
	}
	if match := checkRunnerFailPattern.FindString(output); match != "" {
		return fmt.Errorf("check_runner has failed check %q:\n%s", match, trimmed)
	}
	if strings.Contains(output, "ERROR:") {
		return fmt.Errorf("check_runner has errors:\n%s", trimmed)
	}
	if !checkRunnerOKPattern.MatchString(output) {
		return fmt.Errorf("check_runner output has no successful checks:\n%s", trimmed)
	}
	return nil
}

func assertCheckRunnerCheckAbsent(output, checkName string) error {
	for _, marker := range []string{
		"Running check " + checkName,
		"Check " + checkName + ":",
	} {
		if strings.Contains(output, marker) {
			return fmt.Errorf("check %q unexpectedly ran:\n%s", checkName, strings.TrimSpace(output))
		}
	}
	return nil
}

func assertCheckRunnerCheckOK(output, checkName string) error {
	marker := "Check " + checkName + ": OK"
	if strings.Contains(output, marker) {
		return nil
	}
	return fmt.Errorf("check %q did not complete with OK:\n%s", checkName, strings.TrimSpace(output))
}

func loggedCheckOutputPaths(checkRunnerOutput string) []string {
	seen := make(map[string]struct{})
	var paths []string
	for _, match := range checkRunnerLogPathPattern.FindAllStringSubmatch(checkRunnerOutput, -1) {
		if len(match) != 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		value = strings.TrimPrefix(value, "/mnt/jail")
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		paths = append(paths, value)
	}
	return paths
}

func assertLoggedCheckOutputsExist(ctx context.Context, exec framework.Exec, checkRunnerOutput string) error {
	paths := loggedCheckOutputPaths(checkRunnerOutput)
	if len(paths) == 0 {
		return fmt.Errorf("check_runner output has no check log paths:\n%s", strings.TrimSpace(checkRunnerOutput))
	}
	for _, targetPath := range paths {
		quotedPath := framework.ShellQuote(targetPath)
		if _, err := exec.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf("test -e %s", quotedPath)); err != nil {
			return fmt.Errorf("expected check output %s to exist: %w", targetPath, err)
		}
	}
	return nil
}

func parseHealthCheckReports(output string) ([]healthCheckReport, error) {
	var reports []healthCheckReport
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			continue
		}
		var report healthCheckReport
		if err := json.Unmarshal([]byte(line), &report); err != nil {
			return nil, fmt.Errorf("parse health-check JSON line %q: %w", line, err)
		}
		reports = append(reports, report)
	}
	if len(reports) == 0 {
		return nil, fmt.Errorf("no health-check JSON report lines found in output:\n%s", strings.TrimSpace(output))
	}
	return reports, nil
}

func healthCheckRunID(report healthCheckReport) string {
	if report.Meta == nil {
		return ""
	}
	value, _ := report.Meta["run_id"].(string)
	return strings.TrimSpace(value)
}

func assertHealthCheckReportsPassing(reports []healthCheckReport) ([]string, error) {
	runIDs := make([]string, 0, len(reports))
	for _, report := range reports {
		if report.Status != "PASS" {
			return nil, fmt.Errorf("expected health-check status PASS, got %q", report.Status)
		}
		runID := healthCheckRunID(report)
		if runID == "" {
			return nil, fmt.Errorf("health-check report has empty meta.run_id: %+v", report)
		}
		runIDs = append(runIDs, runID)
	}
	return runIDs, nil
}

func assertHealthCheckRawOutputsPresent(ctx context.Context, exec framework.Exec, runIDs []string) error {
	for _, runID := range runIDs {
		pattern := "*." + runID + ".out"
		cmd := fmt.Sprintf("find %s -type f -name %s -print -quit",
			framework.ShellQuote(healthCheckerStdoutOutputDir),
			framework.ShellQuote(pattern),
		)
		out, err := exec.Jail().RunWithDefaultRetry(ctx, cmd)
		if err != nil {
			return fmt.Errorf("find raw health-check outputs for run_id %s: %w", runID, err)
		}
		if strings.TrimSpace(out) == "" {
			return fmt.Errorf("raw health-check output for run_id %s was not found in %s", runID, healthCheckerStdoutOutputDir)
		}
	}
	return nil
}

func runManualHCProgram(ctx context.Context, exec framework.Exec, worker framework.WorkerRef) error {
	_, err := exec.Worker(worker).RunWithDefaultRetry(ctx,
		fmt.Sprintf("SLURMD_NODENAME=%s /opt/slurm_scripts/hc_program.sh", framework.ShellQuote(worker.Name)))
	if err != nil {
		return fmt.Errorf("run hc_program.sh on %s: %w", worker.Name, err)
	}
	return nil
}
