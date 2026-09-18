package reports

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTeamCityReporterMessages(t *testing.T) {
	var output bytes.Buffer
	reporter := NewTeamCityReporter(&output)

	reporter.TestStarted("suite: feature: scenario", "suite/id")
	reporter.TestFailed("suite: feature: scenario", "expected 'value'\nactual [other] | value", "suite/id")
	reporter.TestIgnored("suite: feature: skipped", "scenario skipped", "suite/skipped")
	reporter.TestFinished("suite: feature: scenario", "suite/id", 1500*time.Millisecond)

	assert.Equal(t, ""+
		"##teamcity[testStarted name='suite: feature: scenario' flowId='suite/id']\n"+
		"##teamcity[testFailed name='suite: feature: scenario' message='expected |'value|'|nactual |[other|] || value' flowId='suite/id']\n"+
		"##teamcity[testIgnored name='suite: feature: skipped' message='scenario skipped' flowId='suite/skipped']\n"+
		"##teamcity[testFinished name='suite: feature: scenario' duration='1500' flowId='suite/id']\n",
		output.String(),
	)
}

func TestTeamCityReporterClampsNegativeDuration(t *testing.T) {
	var output bytes.Buffer
	reporter := NewTeamCityReporter(&output)

	reporter.TestFinished("test", "flow", -time.Second)

	assert.Contains(t, output.String(), " duration='0'")
}

func TestTeamCityReporterWritesConcurrentMessagesAtomically(t *testing.T) {
	var output bytes.Buffer
	reporter := NewTeamCityReporter(&output)

	const count = 100
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reporter.TestStarted("test", "flow")
		}()
	}
	wg.Wait()

	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	assert.Len(t, lines, count)
	for _, line := range lines {
		assert.Equal(t, "##teamcity[testStarted name='test' flowId='flow']", string(line))
	}
}
