//go:build acceptance

package framework

import (
	"fmt"
	"html"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/onsi/ginkgo/v2/types"
)

type StepStatus string

const (
	StepStatusPassed StepStatus = "passed"
	StepStatusFailed StepStatus = "failed"
)

type SummaryReporter struct {
	mu              sync.Mutex
	stepSummaryPath string
	specs           map[string]*specRuntime
	suiteLogs       []string
}

type specRuntime struct {
	steps           []*StepResult
	logs            []string
	activeStepIndex int
}

type activeStep struct {
	specKey   string
	stepIndex int
	startTime time.Time
}

type StepResult struct {
	Name      string
	Status    StepStatus
	Logs      []string
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
}

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func NewSummaryReporter() *SummaryReporter {
	return &SummaryReporter{
		stepSummaryPath: os.Getenv("GITHUB_STEP_SUMMARY"),
		specs:           make(map[string]*specRuntime),
	}
}

func (r *SummaryReporter) StartStep(report types.SpecReport, name string) *activeStep {
	r.mu.Lock()
	defer r.mu.Unlock()

	specKey := report.FullText()
	spec := r.ensureSpecLocked(report)
	spec.steps = append(spec.steps, &StepResult{
		Name:      name,
		StartTime: time.Now(),
	})
	spec.activeStepIndex = len(spec.steps) - 1

	return &activeStep{
		specKey:   specKey,
		stepIndex: spec.activeStepIndex,
		startTime: spec.steps[spec.activeStepIndex].StartTime,
	}
}

func (r *SummaryReporter) FinishStep(token *activeStep, status StepStatus) {
	if token == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	spec, ok := r.specs[token.specKey]
	if !ok || token.stepIndex >= len(spec.steps) {
		return
	}

	step := spec.steps[token.stepIndex]
	step.Status = status
	step.EndTime = time.Now()
	step.Duration = step.EndTime.Sub(token.startTime)

	if spec.activeStepIndex == token.stepIndex {
		spec.activeStepIndex = -1
	}
}

func (r *SummaryReporter) Logf(specKey, line string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	if specKey == "" {
		r.suiteLogs = append(r.suiteLogs, line)
		return
	}

	spec, ok := r.specs[specKey]
	if !ok {
		spec = &specRuntime{activeStepIndex: -1}
		r.specs[specKey] = spec
	}

	spec.logs = append(spec.logs, line)
	if spec.activeStepIndex >= 0 && spec.activeStepIndex < len(spec.steps) {
		spec.steps[spec.activeStepIndex].Logs = append(spec.steps[spec.activeStepIndex].Logs, line)
	}
}

func (r *SummaryReporter) ensureSpecLocked(report types.SpecReport) *specRuntime {
	specKey := report.FullText()
	spec, ok := r.specs[specKey]
	if !ok {
		spec = &specRuntime{activeStepIndex: -1}
		r.specs[specKey] = spec
	}

	return spec
}

func (r *SummaryReporter) WriteSummary(report types.Report) error {
	if r.stepSummaryPath == "" {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var builder strings.Builder
	builder.WriteString("\n")
	builder.WriteString(renderSummaryHeader(report))
	builder.WriteString(renderExecutiveSummary(report))
	builder.WriteString(renderInlineSummary(report, r.specs, r.suiteLogs))

	f, err := os.OpenFile(r.stepSummaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open GITHUB_STEP_SUMMARY: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(builder.String()); err != nil {
		return fmt.Errorf("write acceptance summary: %w", err)
	}

	return nil
}

func renderSummaryHeader(report types.Report) string {
	return fmt.Sprintf("<h2>%s (%s) Acceptance Test Summary</h2>\n\n", reportStatusLabel(report), formatDuration(report.RunTime))
}

func renderExecutiveSummary(report types.Report) string {
	testSpecs := filterTestSpecs(report.SpecReports)
	suites := groupBySuite(testSpecs)
	totalSuites := len(suites)
	if totalSuites == 0 {
		totalSuites = 1
	}
	passedSuites := 0
	for _, suiteReports := range suites {
		if suiteSucceeded(suiteReports) {
			passedSuites++
		}
	}

	totalTests := len(testSpecs)
	if totalTests == 0 {
		totalTests = report.PreRunStats.SpecsThatWillRun
	}
	passedTests := 0
	notRunTests := 0
	for _, spec := range testSpecs {
		if spec.State.Is(types.SpecStatePassed) {
			passedTests++
		}
		if isNotRun(spec) {
			notRunTests++
		}
	}

	interpretation := "All acceptance checks completed successfully."
	if !report.SuiteSucceeded {
		interpretation = "One or more acceptance checks did not complete successfully."
	}
	if len(testSpecs) == 0 && !report.SuiteSucceeded {
		interpretation = fmt.Sprintf("Suite setup failed before any acceptance test could start. %d planned test(s) were not run.", report.PreRunStats.SpecsThatWillRun)
	}
	if notRunTests > 0 {
		interpretation += fmt.Sprintf(" %d test(s) did not run after the suite stopped early.", notRunTests)
	}
	if len(report.SpecialSuiteFailureReasons) > 0 {
		interpretation += " " + strings.Join(report.SpecialSuiteFailureReasons, " ")
	}

	var builder strings.Builder
	builder.WriteString("<ul>\n")
	builder.WriteString(fmt.Sprintf("<li>Suite success rate: <code>%d/%d</code> (%s)</li>\n", passedSuites, totalSuites, formatRate(passedSuites, totalSuites)))
	builder.WriteString(fmt.Sprintf("<li>Test success rate: <code>%d/%d</code> (%s)</li>\n", passedTests, totalTests, formatRate(passedTests, totalTests)))
	builder.WriteString(fmt.Sprintf("<li>Summary: %s</li>\n", html.EscapeString(interpretation)))
	builder.WriteString("</ul>\n\n")

	return builder.String()
}

func renderInlineSummary(report types.Report, specs map[string]*specRuntime, suiteLogs []string) string {
	testSpecs := filterTestSpecs(report.SpecReports)
	if len(testSpecs) == 0 {
		return renderSetupFailure(report, suiteLogs)
	}

	var builder strings.Builder
	if len(suiteLogs) > 0 {
		builder.WriteString(renderLogsDropdown("Suite setup logs", suiteLogs))
	}

	groups := groupBySuite(testSpecs)
	suiteNames := make([]string, 0, len(groups))
	for name := range groups {
		suiteNames = append(suiteNames, name)
	}
	sort.Strings(suiteNames)

	for _, suiteName := range suiteNames {
		suiteReports := groups[suiteName]
		passedTests := 0
		for _, spec := range suiteReports {
			if spec.State.Is(types.SpecStatePassed) {
				passedTests++
			}
		}

		builder.WriteString(fmt.Sprintf("<h3>%s (%s) Suite: %s</h3>\n\n", suiteStatusLabel(suiteReports), formatSuiteDuration(suiteReports), html.EscapeString(suiteName)))
		builder.WriteString("<ul>\n")
		builder.WriteString(fmt.Sprintf("<li>Test success rate: <code>%d/%d</code> (%s)</li>\n", passedTests, len(suiteReports), formatRate(passedTests, len(suiteReports))))
		builder.WriteString("</ul>\n\n")

		for _, spec := range suiteReports {
			runtime := specs[spec.FullText()]
			builder.WriteString(fmt.Sprintf("<h4><strong>%s (%s) %s</strong></h4>\n\n", statusIcon(spec), formatDuration(spec.RunTime), html.EscapeString(spec.LeafNodeText)))
			if spec.Failure.Message != "" {
				builder.WriteString("<p>")
				builder.WriteString(fmt.Sprintf("<strong>Failure summary:</strong> %s", html.EscapeString(sanitizeInline(spec.Failure.Message))))
				builder.WriteString("</p>\n\n")
			}

			if runtime != nil && len(runtime.steps) > 0 {
				for i, step := range runtime.steps {
					builder.WriteString(renderStep(i+1, step))
				}
			}
			builder.WriteString(renderLogsDropdown("Full logs", runtimeLogs(runtime)))
			builder.WriteString("\n")
		}
	}

	return builder.String()
}

func renderSetupFailure(report types.Report, suiteLogs []string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("<h3>%s (%s) Suite Setup</h3>\n\n", reportStatusLabel(report), formatDuration(report.RunTime)))
	builder.WriteString("<ul>\n")
	builder.WriteString(fmt.Sprintf("<li>Planned tests: <code>%d</code></li>\n", report.PreRunStats.SpecsThatWillRun))
	if failureSpec, ok := firstNonTestFailure(report.SpecReports); ok {
		builder.WriteString(fmt.Sprintf("<li>Failure summary: %s</li>\n", html.EscapeString(sanitizeInline(failureSpec.Failure.Message))))
	}
	builder.WriteString("</ul>\n\n")
	if len(suiteLogs) > 0 {
		builder.WriteString(renderLogsDropdown("Suite setup logs", suiteLogs))
	}
	return builder.String()
}

func renderStep(stepNumber int, step *StepResult) string {
	lines := append([]string(nil), step.Logs...)

	title := fmt.Sprintf("%s (%s) Step %d: %s", stepStatusIcon(step.Status), formatDuration(step.Duration), stepNumber, step.Name)

	var builder strings.Builder
	builder.WriteString("<details>\n")
	builder.WriteString(fmt.Sprintf("<summary>%s</summary>\n\n", html.EscapeString(title)))
	builder.WriteString("<pre>\n")
	if len(lines) == 0 {
		builder.WriteString("No step logs were captured.\n")
	} else {
		for _, line := range lines {
			builder.WriteString(html.EscapeString(line))
			builder.WriteByte('\n')
		}
	}
	builder.WriteString("</pre>\n\n</details>\n")
	return builder.String()
}

func renderLogsDropdown(label string, logs []string) string {
	if len(logs) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("<details>\n")
	builder.WriteString(fmt.Sprintf("<summary>%s</summary>\n\n", html.EscapeString(label)))
	builder.WriteString("<pre>\n")
	for _, line := range logs {
		builder.WriteString(html.EscapeString(line))
		builder.WriteByte('\n')
	}
	builder.WriteString("</pre>\n\n</details>\n")
	return builder.String()
}

func runtimeLogs(runtime *specRuntime) []string {
	if runtime == nil {
		return nil
	}
	return runtime.logs
}

func groupBySuite(specReports types.SpecReports) map[string]types.SpecReports {
	out := make(map[string]types.SpecReports)
	for _, spec := range specReports {
		name := suiteNameFor(spec)
		out[name] = append(out[name], spec)
	}
	return out
}

func filterTestSpecs(specReports types.SpecReports) types.SpecReports {
	out := make(types.SpecReports, 0, len(specReports))
	for _, spec := range specReports {
		if spec.LeafNodeType.Is(types.NodeTypeIt) {
			out = append(out, spec)
		}
	}
	return out
}

func firstNonTestFailure(specReports types.SpecReports) (types.SpecReport, bool) {
	for _, spec := range specReports {
		if !spec.LeafNodeType.Is(types.NodeTypeIt) && spec.State.Is(types.SpecStateFailureStates) {
			return spec, true
		}
	}
	return types.SpecReport{}, false
}

func suiteNameFor(spec types.SpecReport) string {
	if len(spec.ContainerHierarchyTexts) > 0 {
		return spec.ContainerHierarchyTexts[0]
	}
	return "Unscoped"
}

func suiteSucceeded(specs types.SpecReports) bool {
	for _, spec := range specs {
		if spec.State.Is(types.SpecStateFailureStates) {
			return false
		}
	}
	return true
}

func suiteStatusLabel(specs types.SpecReports) string {
	if suiteSucceeded(specs) {
		return "[PASS]"
	}
	return "[FAIL]"
}

func isNotRun(spec types.SpecReport) bool {
	return (spec.State.Is(types.SpecStateSkipped) || spec.State.Is(types.SpecStatePending)) && spec.RunTime == 0
}

func formatSuiteDuration(specs types.SpecReports) string {
	if len(specs) == 0 {
		return "0s"
	}
	start := specs[0].StartTime
	end := specs[0].EndTime
	for _, spec := range specs[1:] {
		if spec.StartTime.Before(start) {
			start = spec.StartTime
		}
		if spec.EndTime.After(end) {
			end = spec.EndTime
		}
	}
	if end.Before(start) {
		return "0s"
	}
	return formatDuration(end.Sub(start))
}

func formatDuration(duration time.Duration) string {
	if duration <= 0 {
		return "0s"
	}
	return duration.Round(100 * time.Millisecond).String()
}

func formatRate(passed, total int) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", float64(passed)*100/float64(total))
}

func statusIcon(spec types.SpecReport) string {
	if spec.State.Is(types.SpecStatePassed) {
		return "[PASS]"
	}
	if isNotRun(spec) {
		return "[NOT RUN]"
	}
	return "[FAIL]"
}

func stepStatusIcon(status StepStatus) string {
	if status == StepStatusPassed {
		return "[PASS]"
	}
	return "[FAIL]"
}

func reportStatusLabel(report types.Report) string {
	if report.SuiteSucceeded {
		return "[PASS]"
	}
	return "[FAIL]"
}

func sanitizeInline(value string) string {
	value = stripANSIEscapes(strings.TrimSpace(value))
	return strings.ReplaceAll(value, "\n", " ")
}

func stripANSIEscapes(value string) string {
	value = ansiEscapePattern.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "�[", "")
	return value
}
