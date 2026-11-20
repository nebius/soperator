package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	currentSlurmScriptsOutputDir        = "/opt/soperator-outputs/local/slurm_scripts"
	legacySlurmScriptsOutputDir         = "/opt/soperator-outputs/slurm_scripts"
	currentHealthCheckerStdoutOutputDir = "/opt/soperator-outputs/local/health_checker_cmd_stdout"
	legacyHealthCheckerStdoutOutputDir  = "/opt/soperator-outputs/health_checker_cmd_stdout"
	gpuHealthCheckName                  = "gpu_health_check"
	// check_runner.py logs with time.gmtime and this layout followed by " UTC".
	checkRunnerTimestampLayout = "2006-01-02 15:04:05.000"

	// Slurm runs HealthCheckProgram on its own schedule: with HealthCheckNodeState=CYCLE
	// the next run on a node starts within one HealthCheckInterval, and one more
	// interval covers the check itself and the reason reaching slurmctld.
	periodicHealthCheckMinWait = 2 * time.Minute
	periodicHealthCheckMaxWait = 10 * time.Minute
)

var (
	checkRunnerOKPattern              = regexp.MustCompile(`\bCheck ([^:\n]+): OK\b`)
	checkRunnerFailPattern            = regexp.MustCompile(`\bCheck ([^:\n]+): FAIL\b`)
	checkRunnerFailedCheckLinePattern = regexp.MustCompile(`(?m)^.*\bCheck [^:\n]+: FAIL\b.*$`)
	checkRunnerLogPathPattern         = regexp.MustCompile(`logging to ((?:/mnt/jail)?/\S+)`)
	checkRunnerStartedPattern         = regexp.MustCompile(`(?m)^\s*\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}) UTC\] INFO: Started\s*$`)
	healthCheckConfigFields           = regexp.MustCompile(`(?m)^\s*(HealthCheckProgram|HealthCheckInterval)\s*=\s*(.+)$`)
)

type healthCheckReport struct {
	Status string                  `json:"status"`
	Meta   map[string]any          `json:"meta"`
	Tests  []healthCheckReportTest `json:"tests"`
	// Statuses lists "<test>.<check>" names by status, e.g. {"PASS": [...], "FAIL": [...]}.
	Statuses map[string][]string `json:"statuses"`
	// StatusesByName maps every test and "<test>.<check>" name to its status; disabled ones are "EMPTY".
	StatusesByName map[string]string `json:"statuses_by_name"`
}

type healthCheckReportTest struct {
	Name             string `json:"name"`
	SuppressedStdout bool   `json:"suppressed_stdout"`
	State            struct {
		// Stdout is the file name of the raw command output in the health-checker stdout directory.
		Stdout string `json:"stdout"`
	} `json:"state"`
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

// periodicHealthCheckWait derives how long to wait for Slurm's next periodic HealthCheckProgram run on a
// node from the effective HealthCheckInterval. A disabled or very long interval is reported instead of waited out.
func periodicHealthCheckWait(configuration map[string]string) (time.Duration, error) {
	interval, err := healthCheckInterval(configuration)
	if err != nil {
		return 0, err
	}
	timeout := max(2*interval, periodicHealthCheckMinWait)
	if timeout > periodicHealthCheckMaxWait {
		return 0, fmt.Errorf("HealthCheckInterval %s is too long: waiting %s for a periodic run exceeds the %s limit",
			interval, timeout, periodicHealthCheckMaxWait)
	}
	return timeout, nil
}

// healthCheckInterval returns the effective HealthCheckInterval, failing when HealthCheckProgram is disabled.
func healthCheckInterval(configuration map[string]string) (time.Duration, error) {
	raw, found := configuration["HealthCheckInterval"]
	if !found {
		return 0, fmt.Errorf("HealthCheckInterval is missing from the effective Slurm configuration")
	}
	interval, err := framework.ParseSlurmSeconds(raw)
	if err != nil {
		return 0, fmt.Errorf("parse HealthCheckInterval %q: %w", raw, err)
	}
	if interval == 0 {
		return 0, fmt.Errorf("HealthCheckProgram is disabled: HealthCheckInterval is 0")
	}
	return interval, nil
}

// checkRunnerFailedCheck returns the log line of the check that failed a run. check_runner stops at the
// first failed check and never logs "Finished in" for that run.
func checkRunnerFailedCheck(output string) string {
	return strings.TrimSpace(checkRunnerFailedCheckLinePattern.FindString(output))
}

func waitForHealthyCheckRunnerOutputForJob(ctx context.Context, runtime framework.Runtime, worker framework.WorkerInfo, targetPath, jobID string, timeout time.Duration) (string, error) {
	var content string
	err := runtime.WaitFor(ctx, fmt.Sprintf("healthy check_runner output %s", targetPath), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		out, exists, err := readWorkerFileIfExists(waitCtx, runtime, worker, targetPath)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
		content = out
		if jobID != "" && !checkRunnerOutputMatchesJob(out, jobID) {
			return false, nil
		}
		if !strings.Contains(out, "INFO: Finished in") {
			return false, nil
		}
		if err := assertCheckRunnerHealthy(out); err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		if strings.TrimSpace(content) != "" {
			return "", fmt.Errorf("%w; latest content:\n%s", err, strings.TrimSpace(content))
		}
		return "", err
	}
	return content, nil
}

func waitForPassingHealthCheckReports(ctx context.Context, runtime framework.Runtime, worker framework.WorkerInfo, targetPath, jobID string, previousRunIDs []string, timeout time.Duration) ([]string, error) {
	var content string
	var runIDs []string
	err := runtime.WaitFor(ctx, fmt.Sprintf("passing health-check reports %s", targetPath), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		out, exists, err := readWorkerFileIfExists(waitCtx, runtime, worker, targetPath)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
		content = out
		reports, parseErr := parseHealthCheckReports(out)
		if parseErr == nil {
			if jobID != "" && !healthCheckReportsMatchJob(reports, jobID) {
				return false, nil
			}
			for _, report := range reports {
				if slices.Contains(previousRunIDs, healthCheckRunID(report)) {
					return false, nil
				}
			}
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

// waitForFinishedCheckLog waits until a check log holds a finished run. The check runner truncates a
// check's log when it starts the check, so the log is empty or partial while a run, periodic or not,
// is in progress, and a single read can land in that window.
func waitForFinishedCheckLog(ctx context.Context, runtime framework.Runtime, worker framework.WorkerInfo, targetPath string, finished *regexp.Regexp, timeout time.Duration) (string, error) {
	var content string
	err := runtime.WaitFor(ctx, fmt.Sprintf("finished check log %s", targetPath), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		out, exists, err := readWorkerFileIfExists(waitCtx, runtime, worker, targetPath)
		if err != nil || !exists {
			return false, err
		}
		content = out
		return finished.MatchString(out), nil
	})
	if err == nil {
		return content, nil
	}
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("%s on %s stayed empty or missing (a check run empties it until the run finishes): %w", targetPath, worker.Name, err)
	}
	return "", fmt.Errorf("%s on %s has no finished run: %w; latest content:\n%s", targetPath, worker.Name, err, strings.TrimSpace(content))
}

// readWorkerFileIfExists reads a file from the given worker's jail view. Check outputs can be
// node-local, so they are only guaranteed to be visible on the worker that wrote them.
func readWorkerFileIfExists(ctx context.Context, exec framework.Exec, worker framework.WorkerInfo, targetPath string) (string, bool, error) {
	quotedPath := framework.ShellQuote(targetPath)
	out, err := exec.Worker(worker).RunWithDefaultRetry(ctx, fmt.Sprintf("test -f %s && cat %s || true", quotedPath, quotedPath))
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(out) == "" {
		return "", false, nil
	}
	return out, true, nil
}

func checkRunnerOutputPath(soperatorVersion, worker, contextName string) string {
	return fmt.Sprintf("%s/%s.check_runner.%s.out", slurmScriptsOutputDir(soperatorVersion), worker, contextName)
}

func gpuHealthCheckOutputPath(soperatorVersion, worker, contextName string) string {
	return checkLogPath(soperatorVersion, worker, gpuHealthCheckName, contextName)
}

// checkLogPath is the default per-check log location from the "log" field of a check config.
func checkLogPath(soperatorVersion, worker, checkName, contextName string) string {
	return fmt.Sprintf("%s/%s.%s.%s.out", slurmScriptsOutputDir(soperatorVersion), worker, checkName, contextName)
}

func checkRunnerStartedAt(output string) (time.Time, error) {
	match := checkRunnerStartedPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return time.Time{}, fmt.Errorf("check_runner output does not contain a timestamped start marker:\n%s", strings.TrimSpace(output))
	}
	startedAt, err := time.ParseInLocation(checkRunnerTimestampLayout, match[1], time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse check_runner start time %q: %w", match[1], err)
	}
	return startedAt, nil
}

func slurmScriptsOutputDir(soperatorVersion string) string {
	if framework.SoperatorVersionBeforeFive(soperatorVersion) {
		return legacySlurmScriptsOutputDir
	}
	return currentSlurmScriptsOutputDir
}

func healthCheckerStdoutOutputDir(soperatorVersion string) string {
	if framework.SoperatorVersionBeforeFive(soperatorVersion) {
		return legacyHealthCheckerStdoutOutputDir
	}
	return currentHealthCheckerStdoutOutputDir
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

func checkRunnerOutputMatchesJob(output, jobID string) bool {
	if strings.TrimSpace(jobID) == "" {
		return true
	}
	for _, key := range []string{"SLURM_JOB_ID", "SLURM_JOBID"} {
		if strings.Contains(output, fmt.Sprintf("Environment %s=\"%s\"", key, jobID)) {
			return true
		}
	}
	return false
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

func assertLoggedCheckOutputsExist(ctx context.Context, exec framework.Exec, worker framework.WorkerInfo, checkRunnerOutput string) error {
	paths := loggedCheckOutputPaths(checkRunnerOutput)
	if len(paths) == 0 {
		return fmt.Errorf("check_runner output has no check log paths:\n%s", strings.TrimSpace(checkRunnerOutput))
	}
	for _, targetPath := range paths {
		quotedPath := framework.ShellQuote(targetPath)
		if _, err := exec.Worker(worker).RunWithDefaultRetry(ctx, fmt.Sprintf("test -e %s", quotedPath)); err != nil {
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

func healthCheckMetaString(report healthCheckReport, field string) string {
	value, _ := report.Meta[field].(string)
	return strings.TrimSpace(value)
}

// healthCheckReportTimestamp returns meta.timestamp, the Unix time of the health-checker run.
func healthCheckReportTimestamp(report healthCheckReport) (time.Time, error) {
	value, ok := report.Meta["timestamp"].(float64)
	if !ok || value <= 0 {
		return time.Time{}, fmt.Errorf("health-check report of run %q has no valid meta.timestamp: %v", healthCheckRunID(report), report.Meta["timestamp"])
	}
	return time.Unix(int64(value), 0).UTC(), nil
}

// healthCheckReportProblems summarizes the checks that neither passed nor were disabled.
func healthCheckReportProblems(report healthCheckReport) string {
	var problems []string
	for _, status := range slices.Sorted(maps.Keys(report.Statuses)) {
		if status == "PASS" || status == "EMPTY" {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s: %s", status, strings.Join(report.Statuses[status], ", ")))
	}
	if len(problems) == 0 {
		return "none reported"
	}
	return strings.Join(problems, "; ")
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

func healthCheckReportsMatchJob(reports []healthCheckReport, jobID string) bool {
	if strings.TrimSpace(jobID) == "" {
		return true
	}
	for _, report := range reports {
		if report.Meta == nil {
			return false
		}
		environment, _ := report.Meta["environment"].(string)
		if !environmentHasKeyValue(environment, "SLURM_JOB_ID", jobID) && !environmentHasKeyValue(environment, "SLURM_JOBID", jobID) {
			return false
		}
	}
	return len(reports) > 0
}

// Health-check metadata stores the environment as space-separated KEY=value fields.
// Match whole fields so job ID "2" does not match "23".
func environmentHasKeyValue(environment, key, value string) bool {
	target := key + "=" + value
	for _, field := range strings.Fields(environment) {
		if field == target {
			return true
		}
	}
	return false
}

func assertHealthCheckRawOutputsPresent(ctx context.Context, exec framework.Exec, worker framework.WorkerInfo, soperatorVersion string, runIDs []string) error {
	outputDir := healthCheckerStdoutOutputDir(soperatorVersion)
	for _, runID := range runIDs {
		pattern := "*." + runID + ".out"
		cmd := fmt.Sprintf("find %s -type f -name %s -print -quit",
			framework.ShellQuote(outputDir),
			framework.ShellQuote(pattern),
		)
		out, err := exec.Worker(worker).RunWithDefaultRetry(ctx, cmd)
		if err != nil {
			return fmt.Errorf("find raw health-check outputs for run_id %s: %w", runID, err)
		}
		if strings.TrimSpace(out) == "" {
			return fmt.Errorf("raw health-check output for run_id %s was not found in %s", runID, outputDir)
		}
	}
	return nil
}

func runManualHCProgram(ctx context.Context, exec framework.Exec, worker framework.WorkerInfo) error {
	pod, err := framework.NewKubectlClient(exec).WorkerPodForSlurmNode(ctx, worker.Name)
	if err != nil {
		return fmt.Errorf("find worker pod for %s: %w", worker.Name, err)
	}
	_, err = exec.WorkerPod(pod).RunWithDefaultRetry(ctx,
		fmt.Sprintf("SLURMD_NODENAME=%s /opt/slurm_scripts/hc_program.sh", framework.ShellQuote(worker.Name)))
	if err != nil {
		return fmt.Errorf("run hc_program.sh on %s: %w", worker.Name, err)
	}
	return nil
}

// workerClockNow reads the worker's clock with second precision, rounded down, so it can be compared
// with timestamps the worker writes regardless of the runner's clock.
func workerClockNow(ctx context.Context, exec framework.Exec, worker framework.WorkerInfo) (time.Time, error) {
	out, err := exec.Worker(worker).RunWithDefaultRetry(ctx, "date -u +%s")
	if err != nil {
		return time.Time{}, fmt.Errorf("read clock of %s: %w", worker.Name, err)
	}
	return parseUnixSeconds(out)
}

func parseUnixSeconds(output string) (time.Time, error) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse Unix time %q: %w", strings.TrimSpace(output), err)
	}
	return time.Unix(seconds, 0).UTC(), nil
}

// assertHealthCheckReportRawOutputsPresent requires every raw command output file the report names to exist
// in outputDir on the worker the health-checker ran on.
func assertHealthCheckReportRawOutputsPresent(ctx context.Context, exec framework.Exec, worker framework.WorkerInfo, outputDir string, report healthCheckReport) error {
	files, err := healthCheckReportRawOutputFiles(report)
	if err != nil {
		return err
	}
	out, err := exec.Worker(worker).RunWithDefaultRetry(ctx, missingFilesCommand(outputDir, files))
	if err != nil {
		return fmt.Errorf("look up raw health-check outputs in %s: %w", outputDir, err)
	}
	if missing := strings.Fields(out); len(missing) > 0 {
		return fmt.Errorf("raw health-check outputs of run %s are missing from %s: %s",
			healthCheckRunID(report), outputDir, strings.Join(missing, ", "))
	}
	return nil
}

// healthCheckReportRawOutputFiles lists the raw command output files a report references.
func healthCheckReportRawOutputFiles(report healthCheckReport) ([]string, error) {
	var files []string
	for _, test := range report.Tests {
		name := strings.TrimSpace(test.State.Stdout)
		if test.SuppressedStdout || name == "" {
			continue
		}
		if strings.Contains(name, "/") || name == "." || name == ".." {
			return nil, fmt.Errorf("test %s references raw output %q outside the output directory", test.Name, name)
		}
		if !slices.Contains(files, name) {
			files = append(files, name)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("report of run %s references no raw command outputs", healthCheckRunID(report))
	}
	return files, nil
}

// missingFilesCommand prints the names of the given files that do not exist in dir.
func missingFilesCommand(dir string, files []string) string {
	quoted := make([]string, 0, len(files))
	for _, file := range files {
		quoted = append(quoted, framework.ShellQuote(file))
	}
	return fmt.Sprintf(`cd %s && for f in %s; do [ -f "$f" ] || printf '%%s\n' "$f"; done`,
		framework.ShellQuote(dir), strings.Join(quoted, " "))
}
