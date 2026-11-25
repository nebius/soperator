package sharedsteps

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestSlurmConfigBaselineWaitsForWorkerRecovery(t *testing.T) {
	runtime := &privacyCleanupRuntime{waitAttempts: 2, slurmNodes: []string{
		"NodeName=worker-0 State=IDLE+DRAIN SlurmdStartTime=None",
		"NodeName=worker-0 State=IDLE SlurmdStartTime=2026-11-23T00:00:00",
	}}
	s := &SlurmConfigUpdates{runtime: runtime, slurm: framework.NewSlurmClient(runtime)}
	require.NoError(t, s.recordActiveWorkerStartTimes(t.Context()))
	assert.Equal(t, 2, runtime.slurmReads)
	assert.Equal(t, map[string]time.Time{"worker-0": time.Date(2026, time.November, 23, 0, 0, 0, 0, time.UTC)}, s.workerStartTimes)
}

func TestSlurmConfigBaselineRejectsUnrecoveredWorkers(t *testing.T) {
	runtime := &privacyCleanupRuntime{waitAttempts: 2, slurmNodes: []string{
		"NodeName=worker-0 State=IDLE+NOT_RESPONDING SlurmdStartTime=None",
	}}
	s := &SlurmConfigUpdates{runtime: runtime, slurm: framework.NewSlurmClient(runtime)}
	require.ErrorContains(t, s.recordActiveWorkerStartTimes(t.Context()), "find active workers")
	assert.Empty(t, s.workerStartTimes)
}

func TestCustomSlurmConfigWithJobRequeue(t *testing.T) {
	for name, test := range map[string]struct {
		original *string
		expected string
	}{
		"absent": {
			expected: "JobRequeue=0\n",
		},
		"empty": {
			original: stringPointer(""),
			expected: "JobRequeue=0\n",
		},
		"without trailing newline": {
			original: stringPointer("MinJobAge=60"),
			expected: "MinJobAge=60\nJobRequeue=0\n",
		},
		"with trailing newline": {
			original: stringPointer("MinJobAge=60\n"),
			expected: "MinJobAge=60\nJobRequeue=0\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.expected, customSlurmConfigWithJobRequeue(test.original, "0"))
		})
	}
}

func TestJobRequeue(t *testing.T) {
	value, err := jobRequeue(map[string]string{"JobRequeue": "1"})
	require.NoError(t, err)
	assert.Equal(t, "1", value)

	_, err = jobRequeue(map[string]string{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "find JobRequeue")
}

func TestCompareWorkerStartTimes(t *testing.T) {
	before := time.Date(2026, time.August, 28, 10, 23, 44, 0, time.UTC)
	after := before.Add(time.Minute)

	assert.NoError(t, compareWorkerStartTimes(
		map[string]time.Time{"worker-0": before},
		map[string]time.Time{"worker-0": before},
		false,
	))
	assert.NoError(t, compareWorkerStartTimes(
		map[string]time.Time{"worker-0": before},
		map[string]time.Time{"worker-0": after},
		true,
	))
}

func TestCompareWorkerStartTimesRejectsUnexpectedState(t *testing.T) {
	before := time.Date(2026, time.August, 28, 10, 23, 44, 0, time.UTC)
	after := before.Add(time.Minute)

	err := compareWorkerStartTimes(
		map[string]time.Time{"worker-0": before},
		map[string]time.Time{"worker-0": after},
		false,
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "SlurmdStartTime change")

	err = compareWorkerStartTimes(
		map[string]time.Time{"worker-0": before},
		map[string]time.Time{"worker-0": before},
		true,
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "still at")

	err = compareWorkerStartTimes(
		map[string]time.Time{"worker-0": before},
		map[string]time.Time{},
		true,
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "among active workers")
}

func stringPointer(value string) *string {
	return &value
}
