package reports

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"sync"

	"github.com/cucumber/godog"
)

type cleanupScenario struct {
	uri   string
	order int
	err   error
}

// CleanupFailures tracks execution identities so report correction also handles
// concurrent scenarios and outline rows with identical names.
type CleanupFailures struct {
	mu        sync.Mutex
	scenarios map[string]cleanupScenario
}

func NewCleanupFailures() *CleanupFailures {
	return &CleanupFailures{scenarios: make(map[string]cleanupScenario)}
}

func (c *CleanupFailures) Register(scenario *godog.Scenario) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Godog orders report cases by its numeric pickle IDs.
	order, _ := strconv.Atoi(scenario.Id)
	c.scenarios[scenario.Id] = cleanupScenario{uri: scenario.Uri, order: order}
}

func (c *CleanupFailures) Record(id string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	scenario := c.scenarios[id]
	scenario.err = errors.Join(scenario.err, err)
	c.scenarios[id] = scenario
}

func (c *CleanupFailures) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var failures []error
	for _, scenario := range c.scenarios {
		failures = append(failures, scenario.err)
	}
	return errors.Join(failures...)
}

func (c *CleanupFailures) PromoteReports(dir, suite string) error {
	if dir == "" || c.Err() == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	byURI := make(map[string][]cleanupScenario)
	for _, scenario := range c.scenarios {
		byURI[scenario.uri] = append(byURI[scenario.uri], scenario)
	}
	for _, scenarios := range byURI {
		sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].order < scenarios[j].order })
	}
	cucumberPath := filepath.Join(dir, suite+".cucumber.json")
	data, err := os.ReadFile(cucumberPath)
	if err != nil {
		return fmt.Errorf("read cleanup Cucumber report: %w", err)
	}
	var features []map[string]any
	if err := json.Unmarshal(data, &features); err != nil {
		return fmt.Errorf("decode cleanup Cucumber report: %w", err)
	}
	junitDir := dir
	if TeamCityEnabled() {
		junitDir = filepath.Join(dir, "junit")
	}
	junitPath := filepath.Join(junitDir, suite+".junit.xml")
	data, err = os.ReadFile(junitPath)
	if err != nil {
		return fmt.Errorf("read cleanup JUnit report: %w", err)
	}
	var junit cleanupJUnitReport
	if err := xml.Unmarshal(data, &junit); err != nil {
		return fmt.Errorf("decode cleanup JUnit report: %w", err)
	}
	// Godog emits the same feature and case order in Cucumber and JUnit.
	for i, feature := range features {
		scenarios := byURI[feature["uri"].(string)]
		caseIndex := 0
		for j, raw := range feature["elements"].([]any) {
			steps := raw.(map[string]any)["steps"].([]any)
			if len(steps) == 0 {
				continue
			}
			scenario := scenarios[caseIndex]
			caseIndex++
			if scenario.err == nil {
				continue
			}
			tc := &junit.Suites[i].Cases[j]
			if tc.Failure != nil || slices.ContainsFunc(tc.Errors, func(issue cleanupJUnitIssue) bool { return issue.Type != "skipped" }) {
				continue
			}
			result := steps[len(steps)-1].(map[string]any)["result"].(map[string]any)
			result["status"] = "failed"
			message := scenario.err.Error()
			if original, ok := result["error_message"].(string); ok {
				message = original + "\n" + message
			}
			result["error_message"] = message
			junit.Failures++
			junit.Suites[i].Failures++
			tc.Status = "failed"
			tc.Failure = &cleanupJUnitIssue{Message: message}
			tc.Errors = slices.DeleteFunc(tc.Errors, func(issue cleanupJUnitIssue) bool { return issue.Type == "skipped" })
		}
	}
	data, err = json.MarshalIndent(features, "", "    ")
	if err != nil {
		return fmt.Errorf("encode cleanup Cucumber report: %w", err)
	}
	if err := os.WriteFile(cucumberPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write cleanup Cucumber report: %w", err)
	}
	data, err = xml.MarshalIndent(junit, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cleanup JUnit report: %w", err)
	}
	if err := os.WriteFile(junitPath, append(append([]byte(xml.Header), data...), '\n'), 0o644); err != nil {
		return fmt.Errorf("write cleanup JUnit report: %w", err)
	}
	return nil
}

type cleanupJUnitTotals struct {
	Tests    int    `xml:"tests,attr"`
	Skipped  int    `xml:"skipped,attr"`
	Failures int    `xml:"failures,attr"`
	Errors   int    `xml:"errors,attr"`
	Time     string `xml:"time,attr"`
}

type cleanupJUnitReport struct {
	XMLName xml.Name `xml:"testsuites"`
	Name    string   `xml:"name,attr"`
	cleanupJUnitTotals
	Suites []struct {
		Name string `xml:"name,attr"`
		cleanupJUnitTotals
		Cases []struct {
			Name    string              `xml:"name,attr"`
			Status  string              `xml:"status,attr"`
			Time    string              `xml:"time,attr"`
			Failure *cleanupJUnitIssue  `xml:"failure,omitempty"`
			Errors  []cleanupJUnitIssue `xml:"error,omitempty"`
		} `xml:"testcase"`
	} `xml:"testsuite"`
}

type cleanupJUnitIssue struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr,omitempty"`
}
