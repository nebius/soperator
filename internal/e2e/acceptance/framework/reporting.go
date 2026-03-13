//go:build acceptance

package framework

import (
	"fmt"
	"html"
	"os"
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
	details map[string]string
	steps   []*StepResult
	logs    []string
}

type activeStep struct {
	specKey   string
	stepIndex int
	startTime time.Time
}

type Detail struct {
	Key   string
	Value string
}

type StepResult struct {
	Name      string
	Status    StepStatus
	Failure   string
	Details   []Detail
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
}

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

	return &activeStep{
		specKey:   specKey,
		stepIndex: len(spec.steps) - 1,
		startTime: spec.steps[len(spec.steps)-1].StartTime,
	}
}

func (r *SummaryReporter) AddStepDetail(token *activeStep, key, value string) {
	if token == nil {
		return
	}

	detail, ok := newStepDetail(key, value)
	if !ok {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	spec, ok := r.specs[token.specKey]
	if !ok || token.stepIndex >= len(spec.steps) {
		return
	}

	spec.steps[token.stepIndex].Details = append(spec.steps[token.stepIndex].Details, detail)
}

func (r *SummaryReporter) AddSpecDetail(report types.SpecReport, key, value string) {
	detail, ok := newStepDetail(key, value)
	if !ok {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	spec := r.ensureSpecLocked(report)
	if spec.details == nil {
		spec.details = make(map[string]string)
	}
	spec.details[detail.Key] = detail.Value
}

func (r *SummaryReporter) FinishStep(token *activeStep, status StepStatus, failure string) {
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
	step.Failure = strings.TrimSpace(failure)
	step.EndTime = time.Now()
	step.Duration = step.EndTime.Sub(token.startTime)
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
		spec = &specRuntime{}
		r.specs[specKey] = spec
	}
	spec.logs = append(spec.logs, line)
}

func (r *SummaryReporter) ensureSpecLocked(report types.SpecReport) *specRuntime {
	specKey := report.FullText()
	spec, ok := r.specs[specKey]
	if !ok {
		spec = &specRuntime{}
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
	builder.WriteString("\n## Acceptance Test Summary\n\n")
	builder.WriteString(renderExecutiveSummary(report))
	builder.WriteString(renderSuiteBreakdown(report, r.specs))
	builder.WriteString(renderTechnicalAppendix(report, r.specs, r.suiteLogs))

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

	status := "PASS"
	interpretation := "All acceptance checks completed successfully."
	if !report.SuiteSucceeded {
		status = "FAIL"
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
	builder.WriteString(fmt.Sprintf("**Overall status:** %s\n\n", status))
	builder.WriteString(fmt.Sprintf("- Total elapsed time: `%s`\n", formatDuration(report.RunTime)))
	builder.WriteString(fmt.Sprintf("- Suite success rate: `%d/%d` (%s)\n", passedSuites, totalSuites, formatRate(passedSuites, totalSuites)))
	builder.WriteString(fmt.Sprintf("- Test success rate: `%d/%d` (%s)\n", passedTests, totalTests, formatRate(passedTests, totalTests)))
	builder.WriteString(fmt.Sprintf("- Summary: %s\n\n", interpretation))

	return builder.String()
}

func renderSuiteBreakdown(report types.Report, specs map[string]*specRuntime) string {
	testSpecs := filterTestSpecs(report.SpecReports)
	if len(testSpecs) == 0 {
		return renderSetupFailure(report)
	}

	groups := groupBySuite(testSpecs)
	suiteNames := make([]string, 0, len(groups))
	for name := range groups {
		suiteNames = append(suiteNames, name)
	}
	sort.Strings(suiteNames)

	var builder strings.Builder
	for _, suiteName := range suiteNames {
		suiteReports := groups[suiteName]
		passedTests := 0
		for _, spec := range suiteReports {
			if spec.State.Is(types.SpecStatePassed) {
				passedTests++
			}
		}

		builder.WriteString(fmt.Sprintf("### Suite: %s\n\n", suiteName))
		builder.WriteString(fmt.Sprintf("- Status: %s\n", suiteStatusText(suiteReports)))
		builder.WriteString(fmt.Sprintf("- Elapsed time: `%s`\n", formatSuiteDuration(suiteReports)))
		builder.WriteString(fmt.Sprintf("- Test success rate: `%d/%d` (%s)\n\n", passedTests, len(suiteReports), formatRate(passedTests, len(suiteReports))))

		for _, spec := range suiteReports {
			key := spec.FullText()
			runtime := specs[key]
			builder.WriteString(fmt.Sprintf("#### %s %s\n\n", statusIcon(spec), spec.LeafNodeText))
			builder.WriteString(fmt.Sprintf("- Result: %s\n", humanSpecStatus(spec)))
			builder.WriteString(fmt.Sprintf("- Elapsed time: `%s`\n", formatDuration(spec.RunTime)))
			if spec.Failure.Message != "" {
				builder.WriteString(fmt.Sprintf("- Failure summary: %s\n", sanitizeInline(spec.Failure.Message)))
			}
			builder.WriteString("\n")

			if runtime != nil && len(runtime.steps) > 0 {
				for _, step := range runtime.steps {
					builder.WriteString(fmt.Sprintf("  - %s `%s` %s\n", stepStatusIcon(step.Status), formatDuration(step.Duration), step.Name))
					if step.Status == StepStatusFailed && step.Failure != "" {
						builder.WriteString(fmt.Sprintf("    - Failure: %s\n", sanitizeInline(step.Failure)))
					}
				}
			} else {
				builder.WriteString("  - No step-level activity was recorded for this test.\n")
			}
			builder.WriteString("\n")
		}
	}

	return builder.String()
}

func renderTechnicalAppendix(report types.Report, specs map[string]*specRuntime, suiteLogs []string) string {
	testSpecs := filterTestSpecs(report.SpecReports)

	var builder strings.Builder
	builder.WriteString("### Technical Appendix\n\n")

	if len(suiteLogs) > 0 {
		builder.WriteString("<details>\n")
		builder.WriteString("<summary>Suite setup logs</summary>\n\n")
		builder.WriteString("```text\n")
		for _, line := range suiteLogs {
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
		builder.WriteString("```\n\n")
		builder.WriteString("</details>\n\n")
	}

	for _, spec := range testSpecs {
		runtime := specs[spec.FullText()]
		builder.WriteString("<details>\n")
		builder.WriteString(fmt.Sprintf("<summary>%s (%s)</summary>\n\n", html.EscapeString(spec.LeafNodeText), humanSpecStatus(spec)))
		builder.WriteString(fmt.Sprintf("- Full name: `%s`\n", spec.FullText()))
		builder.WriteString(fmt.Sprintf("- Runtime: `%s`\n", formatDuration(spec.RunTime)))
		builder.WriteString(fmt.Sprintf("- State: `%s`\n", spec.State.String()))
		if spec.Failure.Message != "" {
			builder.WriteString(fmt.Sprintf("- Failure: %s\n", sanitizeInline(spec.Failure.Message)))
		}
		builder.WriteString("\n")

		if runtime != nil && len(runtime.details) > 0 {
			builder.WriteString("**Test details**\n\n")
			keys := make([]string, 0, len(runtime.details))
			for key := range runtime.details {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				builder.WriteString(fmt.Sprintf("- `%s`: `%s`\n", sanitizeInline(key), sanitizeInline(runtime.details[key])))
			}
			builder.WriteString("\n")
		}

		if runtime != nil && len(runtime.steps) > 0 {
			builder.WriteString("**Step details**\n\n")
			for _, step := range runtime.steps {
				builder.WriteString(fmt.Sprintf("- %s `%s` %s\n", stepStatusIcon(step.Status), formatDuration(step.Duration), step.Name))
				if len(step.Details) > 0 {
					sort.Slice(step.Details, func(i, j int) bool {
						return step.Details[i].Key < step.Details[j].Key
					})
					for _, detail := range step.Details {
						builder.WriteString(fmt.Sprintf("  - `%s`: `%s`\n", sanitizeInline(detail.Key), sanitizeInline(detail.Value)))
					}
				}
				if step.Status == StepStatusFailed && step.Failure != "" {
					builder.WriteString("```text\n")
					builder.WriteString(step.Failure)
					if !strings.HasSuffix(step.Failure, "\n") {
						builder.WriteByte('\n')
					}
					builder.WriteString("```\n")
				}
			}
			builder.WriteString("\n")
		}

		if runtime != nil && len(runtime.logs) > 0 {
			builder.WriteString("**Captured logs**\n\n```text\n")
			for _, line := range runtime.logs {
				builder.WriteString(line)
				builder.WriteByte('\n')
			}
			builder.WriteString("```\n\n")
		}

		builder.WriteString("</details>\n\n")
	}

	return builder.String()
}

func renderSetupFailure(report types.Report) string {
	var builder strings.Builder
	builder.WriteString("### Suite Setup\n\n")
	builder.WriteString("- Status: failed before the acceptance tests started\n")
	builder.WriteString(fmt.Sprintf("- Planned tests: `%d`\n", report.PreRunStats.SpecsThatWillRun))
	builder.WriteString(fmt.Sprintf("- Elapsed time: `%s`\n", formatDuration(report.RunTime)))
	if failureSpec, ok := firstNonTestFailure(report.SpecReports); ok {
		builder.WriteString(fmt.Sprintf("- Failure summary: %s\n", sanitizeInline(failureSpec.Failure.Message)))
	}
	builder.WriteString("\n")
	return builder.String()
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

func suiteStatusText(specs types.SpecReports) string {
	if suiteSucceeded(specs) {
		return "passed"
	}
	return "failed"
}

func isNotRun(spec types.SpecReport) bool {
	return (spec.State.Is(types.SpecStateSkipped) || spec.State.Is(types.SpecStatePending)) && spec.RunTime == 0
}

func humanSpecStatus(spec types.SpecReport) string {
	if isNotRun(spec) {
		return "not run"
	}
	return spec.State.String()
}

func newStepDetail(key, value string) (Detail, bool) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return Detail{}, false
	}

	return Detail{
		Key:   key,
		Value: value,
	}, true
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

func sanitizeInline(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 300 {
		return text[:300] + "..."
	}
	return text
}
