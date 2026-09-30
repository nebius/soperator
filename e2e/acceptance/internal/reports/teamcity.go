package reports

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

type teamCityAttribute struct {
	name  string
	value string
}

// TeamCityReporter writes native TeamCity test service messages.
type TeamCityReporter struct {
	out io.Writer
	mu  sync.Mutex
}

// NewTeamCityReporter creates a native TeamCity test reporter.
func NewTeamCityReporter(out io.Writer) *TeamCityReporter {
	return &TeamCityReporter{
		out: out,
	}
}

// TestStarted reports a running test.
func (r *TeamCityReporter) TestStarted(name, flowID string) {
	r.write("testStarted",
		teamCityAttribute{name: "name", value: name},
		teamCityAttribute{name: "flowId", value: flowID},
	)
}

// TestFailed reports a failed test.
func (r *TeamCityReporter) TestFailed(name, message, flowID string) {
	r.write("testFailed",
		teamCityAttribute{name: "name", value: name},
		teamCityAttribute{name: "message", value: message},
		teamCityAttribute{name: "flowId", value: flowID},
	)
}

// TestIgnored reports a skipped test.
func (r *TeamCityReporter) TestIgnored(name, message, flowID string) {
	r.write("testIgnored",
		teamCityAttribute{name: "name", value: name},
		teamCityAttribute{name: "message", value: message},
		teamCityAttribute{name: "flowId", value: flowID},
	)
}

// TestFinished reports a completed test and its elapsed time.
func (r *TeamCityReporter) TestFinished(name, flowID string, duration time.Duration) {
	durationMillis := max(duration.Milliseconds(), 0)
	r.write("testFinished",
		teamCityAttribute{name: "name", value: name},
		teamCityAttribute{name: "duration", value: strconv.FormatInt(durationMillis, 10)},
		teamCityAttribute{name: "flowId", value: flowID},
	)
}

func (r *TeamCityReporter) write(message string, attributes ...teamCityAttribute) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var line strings.Builder
	fmt.Fprintf(&line, "##teamcity[%s", message)
	for _, attribute := range attributes {
		fmt.Fprintf(&line, " %s='%s'", attribute.name, escapeTeamCity(attribute.value))
	}
	line.WriteString("]\n")
	_, _ = io.WriteString(r.out, line.String())
}

func escapeTeamCity(value string) string {
	return strings.NewReplacer(
		"|", "||",
		"'", "|'",
		"\n", "|n",
		"\r", "|r",
		"[", "|[",
		"]", "|]",
	).Replace(value)
}
