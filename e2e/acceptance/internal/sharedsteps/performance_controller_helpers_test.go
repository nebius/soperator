package sharedsteps

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStressSample(t *testing.T) {
	const header = "squeue_ms=120\nsqueue_status=0\nsinfo_ms=45\nsinfo_status=0\nsdiag_ms=250\nsdiag_status=0\n"
	const sdiag = `{"statistics": {"server_thread_count": 2, "jobs_submitted": 40, "jobs_canceled": 0}}` + "\n"
	sample := parseStressSample(header + "states:\n    300 PENDING\n    320 RUNNING\nsdiag:\n" + sdiag)
	assert.Equal(t, stressSample{
		SqueueMillis: 120, SinfoMillis: 45, SdiagMillis: 250, Pending: 300, Running: 320,
		Sdiag: &sdiagStatistics{ServerThreadCount: 2, JobsSubmitted: 40},
	}, sample)

	idle := parseStressSample(header + "states:\n\nsdiag:\n" + sdiag)
	assert.Equal(t, 0, idle.Pending)
	assert.Empty(t, idle.Error)

	broken := parseStressSample("squeue_ms=120\nsqueue_status=1\nsinfo_ms=x\nsinfo_status=0\nsdiag_ms=1\n")
	assert.Equal(t, int64(120), broken.SqueueMillis)
	assert.Contains(t, broken.Error, `sinfo_ms="x"`)
	assert.Contains(t, broken.Error, "sdiag_status missing")
	assert.Contains(t, broken.Error, "no states: section")
	assert.Contains(t, broken.Error, "no sdiag: section")
	assert.Equal(t, "squeue exited 1 after 120 ms", broken.SlurmError, "a nonzero Slurm exit is a failure, not a missing measurement")

	timedOut := parseStressSample("squeue_ms=60210\nsqueue_status=1\nsinfo_ms=60205\nsinfo_status=1\nsdiag_ms=250\nsdiag_status=0\n" +
		"states:\n\nsdiag:\n" + sdiag)
	assert.Empty(t, timedOut.Error)
	assert.Equal(t, "squeue exited 1 after 60210 ms, sinfo exited 1 after 60205 ms", timedOut.SlurmError)

	noSdiag := parseStressSample(header + "states:\n\nsdiag:\nsdiag: error: Unable to contact slurm controller\n")
	assert.Nil(t, noSdiag.Sdiag)
	assert.Contains(t, noSdiag.Error, "parse sdiag JSON")
}

func TestStressProbeCommandRunsProbesConcurrently(t *testing.T) {
	command := stressProbeCommand()
	assert.Contains(t, command, "set -o pipefail; dir=$(mktemp -d); ")
	assert.Contains(t, command, `squeue --noheader --states=PD,R --format='%T' | sort | uniq --count > "$dir/states"; status=$?`)
	assert.Contains(t, command, `sinfo --noheader >/dev/null; status=$?`)
	assert.Contains(t, command, `sdiag --json > "$dir/sdiag"; status=$?`)
	assert.Equal(t, 3, strings.Count(command, `.timing" & `), "each probe runs in its own background subshell")
	assert.NotContains(t, command, "&;", "a semicolon after & is a bash syntax error")
	assert.Contains(t, command, `.timing" & wait; cat "$dir/squeue.timing" "$dir/sinfo.timing" "$dir/sdiag.timing"; `)
	assert.True(t, strings.HasSuffix(command, `; echo states:; cat "$dir/states"; echo sdiag:; cat "$dir/sdiag"; rm -rf "$dir"`), command)
	assert.NotContains(t, command, "\n")
}

func TestEvaluateStressProbes(t *testing.T) {
	at := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	samples := []stressSample{
		{At: at, SqueueMillis: 100, SinfoMillis: 50},
		{At: at.Add(10 * time.Second), SqueueMillis: 900, SinfoMillis: 70},
		{At: at.Add(20 * time.Second), SqueueMillis: 300, SinfoMillis: 60},
	}
	budget := stressProbeBudget{SqueueMean: time.Second, SinfoMean: time.Second, SqueueMax: 5 * time.Second, SinfoMax: 5 * time.Second, SdiagMax: 5 * time.Second}
	require.NoError(t, evaluateStressProbes(summarizeStressProbes(samples), budget))
	require.ErrorContains(t, evaluateStressProbes(summarizeStressProbes(samples[:2]), budget), "only 2 of 2 responsiveness probes ran")

	samples[2].Error = "kubectl exec: dial tcp: i/o timeout"
	require.ErrorContains(t, evaluateStressProbes(summarizeStressProbes(samples), budget), "only 2 of 3 responsiveness probes ran",
		"a probe that could not run is not a controller failure, but enough probes must run")

	samples = append(samples, stressSample{At: at.Add(30 * time.Second), SqueueMillis: 100, SinfoMillis: 60})
	samples[1].SqueueMillis = 7000
	summary := summarizeStressProbes(samples)
	assert.Equal(t, stressProbeSummary{
		Samples: 4, Errors: 1,
		Squeue: stressDistribution{Mean: 2400, P50: 100, P90: 7000, P95: 7000, P99: 7000, Max: 7000},
		Sinfo:  stressDistribution{Mean: 60, P50: 60, P90: 70, P95: 70, P99: 70, Max: 70},
	}, summary, "samples with errors are excluded from the latency percentiles")
	err := evaluateStressProbes(summary, budget)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "squeue mean 2400 ms, budget 1s")
	assert.Contains(t, err.Error(), "squeue max 7000 ms, budget 5s")
	assert.NotContains(t, err.Error(), "sinfo")

	failed := append(samples[:3:3], stressSample{At: at.Add(30 * time.Second), SqueueMillis: 60210, SinfoMillis: 60, SlurmError: "squeue exited 1 after 60210 ms"})
	failedSummary := summarizeStressProbes(failed)
	assert.Equal(t, 1, failedSummary.SlurmErrors)
	assert.Equal(t, "10:00:30: squeue exited 1 after 60210 ms", failedSummary.FirstSlurmError)
	assert.Equal(t, float64(60210), failedSummary.Squeue.Max, "a failed probe keeps its measured latency")
	require.ErrorContains(t, evaluateStressProbes(failedSummary, budget), "controller failed 1 of 4 responsiveness probes during the load, first at 10:00:30: squeue exited 1 after 60210 ms")

	summary.Squeue = stressDistribution{Mean: 1500, P95: 1500, Max: 1500}
	require.ErrorContains(t, evaluateStressProbes(summary, budget), "squeue mean 1500 ms, budget 1s",
		"a slow typical probe fails even below the single-probe budget")

	summary.Squeue = stressDistribution{Mean: 400, P95: 1900, Max: 1900}
	summary.Sinfo = stressDistribution{Mean: 400, P95: 1900, Max: 1900}
	require.NoError(t, evaluateStressProbes(summary, budget), "stalls behind the scheduler pass are not a typical-latency failure")
	summary.Sinfo.Mean = 1200
	require.ErrorContains(t, evaluateStressProbes(summary, budget), "sinfo mean 1200 ms, budget 1s")
	summary.Sinfo.Mean = 400
	summary.Sinfo.Max = 6000
	require.ErrorContains(t, evaluateStressProbes(summary, budget), "sinfo max 6000 ms, budget 5s")
	summary.Sinfo.Max = 100
	summary.Sdiag = stressDistribution{P95: 300, Max: 5500}
	require.ErrorContains(t, evaluateStressProbes(summary, budget), "sdiag max 5500 ms, budget 5s")
	budget.SdiagMax = 6 * time.Second
	require.NoError(t, evaluateStressProbes(summary, budget), "each command has its own max budget")
}

func TestParseSdiag(t *testing.T) {
	statistics, err := parseSdiag(`{
  "statistics": {
    "req_time": {"set": true, "infinite": false, "number": 1793727578},
    "server_thread_count": 3,
    "agent_queue_size": 1,
    "schedule_cycle_sum": 83139,
    "schedule_cycle_total": 1299,
    "schedule_cycle_mean": 64,
    "bf_cycle_counter": 27,
    "bf_cycle_sum": 540000,
    "bf_cycle_mean": 20000,
    "jobs_submitted": 4,
    "jobs_canceled": 2,
    "jobs_failed": 1,
    "bf_active": false
  },
  "warnings": [],
  "errors": []
}`)
	require.NoError(t, err)
	assert.Equal(t, sdiagStatistics{
		ServerThreadCount: 3, AgentQueueSize: 1, ScheduleCycleSum: 83139, ScheduleCycleTotal: 1299,
		BackfillCycleSum: 540000, BackfillCycleCounter: 27,
		JobsSubmitted: 4, JobsCanceled: 2, JobsFailed: 1,
	}, statistics)

	_, err = parseSdiag("sdiag: error: Unable to contact slurm controller")
	require.ErrorContains(t, err, "parse sdiag JSON")
}

func TestSummarizeStressScheduler(t *testing.T) {
	before := sdiagStatistics{ServerThreadCount: 1, ScheduleCycleSum: 83139, ScheduleCycleTotal: 1299, BackfillCycleSum: 1000, BackfillCycleCounter: 10, JobsSubmitted: 4}
	after := sdiagStatistics{ServerThreadCount: 3, ScheduleCycleSum: 583139, ScheduleCycleTotal: 1399, BackfillCycleSum: 401000, BackfillCycleCounter: 30, JobsSubmitted: 1004}

	summary := summarizeStressScheduler([]sdiagStatistics{before, after})
	assert.Equal(t, stressSchedulerSummary{
		JobsSubmitted:         1000,
		ScheduleCycles:        100,
		ScheduleCycleMeanSecs: 0.005,
		BackfillCycles:        20,
		BackfillCycleMeanSecs: 0.02,
		ServerThreadCount:     3,
	}, summary)
}

func TestSummarizeStressSchedulerAcrossDailyReset(t *testing.T) {
	snapshots := []sdiagStatistics{
		{ScheduleCycleSum: 83139, ScheduleCycleTotal: 1299, JobsSubmitted: 5000, JobsCanceled: 1, JobsFailed: 2},
		{ScheduleCycleSum: 84000, ScheduleCycleTotal: 1310, JobsSubmitted: 5500, JobsCanceled: 1, JobsFailed: 2},
		// The daily reset happened in this interval: the counters restarted from zero.
		{ScheduleCycleSum: 500, ScheduleCycleTotal: 5, JobsSubmitted: 30, JobsFailed: 1},
		{ScheduleCycleSum: 2000, ScheduleCycleTotal: 20, JobsSubmitted: 1000, JobsFailed: 1, ServerThreadCount: 4},
	}

	summary := summarizeStressScheduler(snapshots)
	assert.Equal(t, 1, summary.Resets)
	assert.Equal(t, int64(500+30+970), summary.JobsSubmitted, "the interval with the reset contributes its absolutes")
	assert.Equal(t, int64(1), summary.JobsFailed)
	assert.Equal(t, int64(0), summary.JobsCanceled)
	assert.Equal(t, int64(11+5+15), summary.ScheduleCycles)
	assert.InDelta(t, float64(861+500+1500)/31/1e6, summary.ScheduleCycleMeanSecs, 1e-12)
	assert.Equal(t, int64(4), summary.ServerThreadCount)

	gaugesOnly := summarizeStressScheduler([]sdiagStatistics{
		{JobsSubmitted: 10, ServerThreadCount: 9, AgentQueueSize: 3},
		{JobsSubmitted: 12, ServerThreadCount: 1, AgentQueueSize: 0},
	})
	assert.Equal(t, 0, gaugesOnly.Resets, "gauges dropping is not a reset")
	assert.Equal(t, int64(2), gaugesOnly.JobsSubmitted)

	canceledDrop := summarizeStressScheduler([]sdiagStatistics{{JobsSubmitted: 10, JobsCanceled: 3}, {JobsSubmitted: 11, JobsCanceled: 0}})
	assert.Equal(t, 1, canceledDrop.Resets, "every cumulative counter is watched")
	backfillDrop := summarizeStressScheduler([]sdiagStatistics{{JobsSubmitted: 10, BackfillCycleCounter: 3}, {JobsSubmitted: 11, BackfillCycleCounter: 1}})
	assert.Equal(t, 1, backfillDrop.Resets)
}

func TestEvaluateStressScheduler(t *testing.T) {
	summary := stressSchedulerSummary{
		JobsSubmitted: 3, JobsFailed: 2, JobsCanceled: 1,
		ScheduleCycles: 10, ScheduleCycleMeanSecs: 2.5, BackfillCycles: 4, BackfillCycleMeanSecs: 0.3,
		AgentQueueSize: 4, ServerThreadCount: 20,
	}
	budget := stressSchedulerBudget{MinSubmitted: 5, ScheduleCycleMean: time.Second, BackfillCycleMean: 200 * time.Millisecond, MaxServerThreads: 10}
	err := evaluateStressScheduler(summary, budget)
	require.Error(t, err)
	for _, expected := range []string{
		"counted 3 submitted jobs, expected at least 5",
		"main scheduler mean cycle 2.500s over 10 cycles exceeds budget 1s",
		"backfill scheduler mean cycle 0.300s over 4 cycles exceeds budget 200ms",
		"agent queue still holds 4 requests",
		"still runs 20 server threads, expected at most 10",
	} {
		assert.Contains(t, err.Error(), expected)
	}

	err = evaluateStressScheduler(stressSchedulerSummary{JobsSubmitted: 5}, budget)
	require.ErrorContains(t, err, "recorded no main scheduler cycles")
	require.ErrorContains(t, err, "recorded no backfill scheduler cycles")
	healthy := stressSchedulerSummary{JobsSubmitted: 5, JobsFailed: 2, JobsCanceled: 1, ScheduleCycles: 10, ScheduleCycleMeanSecs: 0.5, BackfillCycles: 4, BackfillCycleMeanSecs: 0.1}
	require.NoError(t, evaluateStressScheduler(healthy, budget),
		"failed and canceled jobs of other users are not the load's problem")
	require.NoError(t, evaluateStressScheduler(stressSchedulerSummary{Resets: 1, JobsSubmitted: 2, ScheduleCycles: 10, ScheduleCycleMeanSecs: 0.5, BackfillCycles: 4},
		budget),
		"a counter reset during the load loses submissions, so the count is not asserted after one")
	require.ErrorContains(t, evaluateStressScheduler(stressSchedulerSummary{Resets: 1, JobsSubmitted: 2, ScheduleCycles: 10, ScheduleCycleMeanSecs: 5, BackfillCycles: 4},
		budget),
		"main scheduler mean cycle", "the cycle budgets still apply across a reset")
}
