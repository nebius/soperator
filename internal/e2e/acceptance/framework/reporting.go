//go:build acceptance

package framework

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
)

type StepStatus string

const (
	StepStatusPassed StepStatus = "passed"
	StepStatusFailed StepStatus = "failed"
)

const (
	reportEntryStep      = "acceptance-step"
	reportEntrySpecLogs  = "acceptance-full-logs"
	reportEntrySetupLogs = "acceptance-setup-logs"
)

var preferredSuiteOrder = []string{
	"Simple acceptance",
	"Node replacement acceptance",
}

type SummaryReporter struct {
	mu              sync.Mutex
	stepSummaryPath string
	logs            []string
	activeStep      *activeStep
	nextStepNumber  int
}

type activeStep struct {
	Number    int
	Keyword   string
	Name      string
	StartTime time.Time
	Logs      []string
}

type StepResult struct {
	Number   int      `json:"number"`
	Keyword  string   `json:"keyword"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Duration string   `json:"duration"`
	Logs     []string `json:"logs,omitempty"`
}

type LogsResult struct {
	Logs []string `json:"logs,omitempty"`
}

type suiteGroup struct {
	Name  string
	Specs types.SpecReports
}

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func NewSummaryReporter() *SummaryReporter {
	return &SummaryReporter{
		stepSummaryPath: os.Getenv("GITHUB_STEP_SUMMARY"),
	}
}

func (r *SummaryReporter) BeginNode() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logs = nil
	r.activeStep = nil
	r.nextStepNumber = 0
}

func (r *SummaryReporter) StartStep(keyword, name string) *activeStep {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextStepNumber++
	r.activeStep = &activeStep{
		Number:    r.nextStepNumber,
		Keyword:   keyword,
		Name:      name,
		StartTime: time.Now(),
	}

	token := *r.activeStep
	token.Logs = nil

	return &token
}

func (r *SummaryReporter) FinishStep(token *activeStep, status StepStatus) {
	if token == nil {
		return
	}

	r.mu.Lock()
	payload := StepResult{
		Number:   token.Number,
		Keyword:  token.Keyword,
		Name:     token.Name,
		Status:   string(status),
		Duration: formatDuration(time.Since(token.StartTime)),
	}
	if r.activeStep != nil && r.activeStep.Number == token.Number {
		payload.Logs = append([]string(nil), r.activeStep.Logs...)
		r.activeStep = nil
	}
	r.mu.Unlock()

	r.addJSONReportEntry(reportEntryStep, payload)
}

func (r *SummaryReporter) Logf(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	r.logs = append(r.logs, line)
	if r.activeStep != nil {
		r.activeStep.Logs = append(r.activeStep.Logs, line)
	}
}

func (r *SummaryReporter) FlushSpecLogs() {
	r.flushLogs(reportEntrySpecLogs)
}

func (r *SummaryReporter) FlushSetupLogs() {
	r.flushLogs(reportEntrySetupLogs)
}

func (r *SummaryReporter) flushLogs(entryName string) {
	r.mu.Lock()
	payload := LogsResult{
		Logs: append([]string(nil), r.logs...),
	}
	r.logs = nil
	r.mu.Unlock()

	if len(payload.Logs) == 0 {
		return
	}

	r.addJSONReportEntry(entryName, payload)
}

func (r *SummaryReporter) addJSONReportEntry(name string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		ginkgo.Fail(fmt.Sprintf("Failed to encode acceptance report entry %q: %v", name, err), 1)
	}

	ginkgo.AddReportEntry(name, string(data), ginkgo.ReportEntryVisibilityNever)
}

func (r *SummaryReporter) WriteSummary(report types.Report) error {
	if r.stepSummaryPath == "" {
		return nil
	}

	var builder strings.Builder
	builder.WriteString("\n")
	builder.WriteString(renderSummaryHeader(report))
	builder.WriteString(renderExecutiveSummary(report))
	builder.WriteString(renderInlineSummary(report))

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
	return fmt.Sprintf("<h2>Acceptance Test Summary %s</h2>\n\n", formatHeaderSuffix(reportStatusText(report), report.RunTime))
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

func renderInlineSummary(report types.Report) string {
	testSpecs := filterTestSpecs(report.SpecReports)
	if len(testSpecs) == 0 {
		return renderSetupFailure(report)
	}

	var builder strings.Builder
	setupLogs := setupLogs(report.SpecReports)
	if len(setupLogs) > 0 {
		builder.WriteString(renderLogsDropdown("Suite setup logs", setupLogs))
	}

	for _, group := range orderedSuiteGroups(testSpecs) {
		passedTests := 0
		for _, spec := range group.Specs {
			if spec.State.Is(types.SpecStatePassed) {
				passedTests++
			}
		}

		builder.WriteString(fmt.Sprintf("<h3>Suite: %s %s</h3>\n\n", html.EscapeString(group.Name), formatHeaderSuffix(suiteStatusText(group.Specs), suiteDuration(group.Specs))))
		builder.WriteString("<ul>\n")
		builder.WriteString(fmt.Sprintf("<li>Test success rate: <code>%d/%d</code> (%s)</li>\n", passedTests, len(group.Specs), formatRate(passedTests, len(group.Specs))))
		builder.WriteString("</ul>\n\n")

		for _, spec := range group.Specs {
			builder.WriteString(fmt.Sprintf("<h4><strong>%s %s</strong></h4>\n\n", html.EscapeString(spec.LeafNodeText), formatHeaderSuffix(specStatusText(spec), spec.RunTime)))
			if spec.Failure.Message != "" {
				builder.WriteString("<p>")
				builder.WriteString(fmt.Sprintf("<strong>Failure summary:</strong> %s", html.EscapeString(sanitizeInline(spec.Failure.Message))))
				builder.WriteString("</p>\n\n")
			}

			for _, step := range stepResults(spec) {
				builder.WriteString(renderStep(step))
			}
			builder.WriteString(renderLogsDropdown("Full logs", specLogs(spec)))
			builder.WriteString("\n")
		}
	}

	return builder.String()
}

func renderSetupFailure(report types.Report) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("<h3>Suite Setup %s</h3>\n\n", formatHeaderSuffix(reportStatusText(report), report.RunTime)))
	builder.WriteString("<ul>\n")
	builder.WriteString(fmt.Sprintf("<li>Planned tests: <code>%d</code></li>\n", report.PreRunStats.SpecsThatWillRun))
	if failureSpec, ok := firstNonTestFailure(report.SpecReports); ok {
		builder.WriteString(fmt.Sprintf("<li>Failure summary: %s</li>\n", html.EscapeString(sanitizeInline(failureSpec.Failure.Message))))
	}
	builder.WriteString("</ul>\n\n")
	if logs := setupLogs(report.SpecReports); len(logs) > 0 {
		builder.WriteString(renderLogsDropdown("Suite setup logs", logs))
	}
	return builder.String()
}

func renderStep(step StepResult) string {
	title := fmt.Sprintf("%d. %s %s", step.Number, renderStepName(step), formatHeaderSuffix(stepStatusText(StepStatus(step.Status)), parseDuration(step.Duration)))

	var builder strings.Builder
	builder.WriteString("<details>\n")
	builder.WriteString(fmt.Sprintf("<summary>%s</summary>\n\n", title))
	builder.WriteString("<pre>\n")
	if len(step.Logs) == 0 {
		builder.WriteString("No step logs were captured.\n")
	} else {
		for _, line := range step.Logs {
			builder.WriteString(html.EscapeString(line))
			builder.WriteByte('\n')
		}
	}
	builder.WriteString("</pre>\n\n</details>\n")
	return builder.String()
}

func renderStepName(step StepResult) string {
	if step.Keyword == "" {
		return html.EscapeString(step.Name)
	}

	return fmt.Sprintf("<strong>%s</strong> %s", html.EscapeString(step.Keyword), html.EscapeString(step.Name))
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

func stepResults(spec types.SpecReport) []StepResult {
	results := make([]StepResult, 0)
	for _, entry := range spec.ReportEntries {
		if entry.Name != reportEntryStep {
			continue
		}

		var step StepResult
		if decodeReportEntry(entry, &step) {
			results = append(results, step)
		}
	}

	return results
}

func specLogs(spec types.SpecReport) []string {
	logs := make([]string, 0)
	for _, entry := range spec.ReportEntries {
		if entry.Name != reportEntrySpecLogs {
			continue
		}

		var payload LogsResult
		if decodeReportEntry(entry, &payload) {
			logs = append(logs, payload.Logs...)
		}
	}

	return logs
}

func setupLogs(specReports types.SpecReports) []string {
	logs := make([]string, 0)
	for _, spec := range specReports {
		if spec.LeafNodeType.Is(types.NodeTypeIt) {
			continue
		}
		for _, entry := range spec.ReportEntries {
			if entry.Name != reportEntrySetupLogs {
				continue
			}

			var payload LogsResult
			if decodeReportEntry(entry, &payload) {
				logs = append(logs, payload.Logs...)
			}
		}
	}

	return logs
}

func decodeReportEntry(entry types.ReportEntry, out any) bool {
	value, ok := entry.GetRawValue().(string)
	if !ok {
		value = entry.StringRepresentation()
	}
	if value == "" {
		return false
	}

	return json.Unmarshal([]byte(value), out) == nil
}

func orderedSuiteGroups(specReports types.SpecReports) []suiteGroup {
	groups := groupBySuite(specReports)
	orderedNames := make([]string, 0, len(groups))
	seen := make(map[string]struct{}, len(groups))

	for _, name := range preferredSuiteOrder {
		if _, ok := groups[name]; ok {
			orderedNames = append(orderedNames, name)
			seen[name] = struct{}{}
		}
	}

	for _, spec := range specReports {
		name := suiteNameFor(spec)
		if _, ok := seen[name]; ok {
			continue
		}
		if _, ok := groups[name]; !ok {
			continue
		}
		orderedNames = append(orderedNames, name)
		seen[name] = struct{}{}
	}

	ordered := make([]suiteGroup, 0, len(orderedNames))
	for _, name := range orderedNames {
		ordered = append(ordered, suiteGroup{Name: name, Specs: groups[name]})
	}

	return ordered
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
		return "PASS"
	}
	return "FAIL"
}

func isNotRun(spec types.SpecReport) bool {
	return (spec.State.Is(types.SpecStateSkipped) || spec.State.Is(types.SpecStatePending)) && spec.RunTime == 0
}

func suiteDuration(specs types.SpecReports) time.Duration {
	if len(specs) == 0 {
		return 0
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
		return 0
	}
	return end.Sub(start)
}

func formatDuration(duration time.Duration) string {
	if duration <= 0 {
		return "0s"
	}
	return duration.Round(100 * time.Millisecond).String()
}

func parseDuration(value string) time.Duration {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return parsed
}

func formatRate(passed, total int) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", float64(passed)*100/float64(total))
}

func specStatusText(spec types.SpecReport) string {
	if spec.State.Is(types.SpecStatePassed) {
		return "PASS"
	}
	if isNotRun(spec) {
		return "NOT RUN"
	}
	return "FAIL"
}

func stepStatusText(status StepStatus) string {
	if status == StepStatusPassed {
		return "PASS"
	}
	return "FAIL"
}

func reportStatusText(report types.Report) string {
	if report.SuiteSucceeded {
		return "PASS"
	}
	return "FAIL"
}

func formatHeaderSuffix(status string, duration time.Duration) string {
	return fmt.Sprintf("(%s, %s)", status, formatDuration(duration))
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
