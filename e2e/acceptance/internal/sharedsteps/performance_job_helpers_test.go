package sharedsteps

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStressSacctCommands(t *testing.T) {
	const filter = "env SLURM_TIME_FORMAT=%s sacct --noheader --parsable2 --name='e2e-stress-burst-1' --user='stress' --starttime=now-2hours "
	assert.Equal(t, filter+"--format=JobID,State,ExitCode,Restarts,Submit,Eligible,Start,End,NodeList", stressSacctCommand("e2e-stress-burst-1"))
	assert.Equal(t, filter+"--allocations --format=State | sort | uniq --count", stressSacctCountCommand("e2e-stress-burst-1"))
}

func TestStressAccountingSettled(t *testing.T) {
	settled, err := stressAccountingSettled("   1000 COMPLETED\n", 1000)
	require.NoError(t, err)
	assert.True(t, settled)

	settled, err = stressAccountingSettled("    990 COMPLETED\n      5 RUNNING\n      3 COMPLETING\n", 1000)
	require.ErrorContains(t, err, "sacct has 998 of 1000 records, alive: COMPLETING=3 RUNNING=5")
	assert.False(t, settled)

	settled, err = stressAccountingSettled("", 1000)
	require.ErrorContains(t, err, "sacct has 0 of 1000 records")
	assert.False(t, settled)
}

func TestParseStressJobRecords(t *testing.T) {
	output := strings.Join([]string{
		"100|COMPLETED|0:0|0|1000|1000|1001|1010|worker-0",
		"100.batch|COMPLETED|0:0||1001|1001|1001|1010|worker-0",
		"100.extern|COMPLETED|0:0||1001|1001|1001|1012|worker-0",
		"100.0|COMPLETED|0:0||1003|1003|1003|1008|worker-0",
		"100.1|COMPLETED|0:0||1008|1008|1008|1009|worker-0",
		"101|COMPLETED|0:0|1|1000|1000|1003|1004|worker-1",
		"101.batch|COMPLETED|0:0||1003|1003|1003|1004|worker-1",
		"102|FAILED|1:0|0|1000|1000|None|1000|None assigned",
		"103.0|COMPLETED|0:0||1000|1000|1000|1001|worker-1",
		"",
	}, "\n")

	records, err := parseStressJobRecords(output)
	require.NoError(t, err)
	require.Len(t, records, 3)
	assert.Equal(t, stressJobRecord{
		ID: "100", State: "COMPLETED", ExitCode: "0:0", NodeList: "worker-0",
		Submit: time.Unix(1000, 0), Eligible: time.Unix(1000, 0), Start: time.Unix(1001, 0), End: time.Unix(1010, 0),
		FirstStepStart: time.Unix(1003, 0), LastStepEnd: time.Unix(1009, 0), ExternEnd: time.Unix(1012, 0),
	}, records[0])
	assert.Equal(t, "101", records[1].ID)
	assert.Equal(t, 1, records[1].Restarts)
	assert.Equal(t, time.Unix(1003, 0), records[1].FirstStepStart, "batch step stands in when there is no srun step")
	assert.Equal(t, time.Unix(1004, 0), records[1].LastStepEnd)
	assert.True(t, records[1].ExternEnd.IsZero())
	assert.Equal(t, "102", records[2].ID)
	assert.True(t, records[2].Start.IsZero())

	_, err = parseStressJobRecords("100|COMPLETED|0:0")
	require.ErrorContains(t, err, "unexpected sacct row")

	_, err = parseStressJobRecords("100|COMPLETED|0:0|0|yesterday|1|1|2|worker-0")
	require.ErrorContains(t, err, "parse timestamp of job 100")

	_, err = parseStressJobRecords("100|COMPLETED|0:0||1|1|1|2|worker-0")
	require.ErrorContains(t, err, "parse restarts of job 100")

	_, err = parseStressJobRecords("100.0|COMPLETED|0:0||1|1|soon|2|worker-0")
	require.ErrorContains(t, err, "parse timestamp of step 100.0")
}

func stressTestRecord(id string, submit, start, end int64, node string) stressJobRecord {
	return stressJobRecord{
		ID: id, State: "COMPLETED", ExitCode: "0:0", NodeList: node,
		Submit: time.Unix(submit, 0), Eligible: time.Unix(submit, 0), Start: time.Unix(start, 0), End: time.Unix(end, 0),
	}
}

func stressTestRecordWithSteps(id string, submit, start, firstStep, lastStep, end, extern int64, node string) stressJobRecord {
	record := stressTestRecord(id, submit, start, end, node)
	record.FirstStepStart = time.Unix(firstStep, 0)
	record.LastStepEnd = time.Unix(lastStep, 0)
	if extern > 0 {
		record.ExternEnd = time.Unix(extern, 0)
	}
	return record
}

func TestStressJobProblems(t *testing.T) {
	require.NoError(t, stressJobProblems([]stressJobRecord{stressTestRecord("1", 0, 1, 2, "worker-0")}))

	failed := stressTestRecord("2", 0, 1, 2, "worker-1")
	failed.State = "FAILED"
	failed.ExitCode = "1:0"
	unstarted := stressTestRecord("3", 0, 1, 2, "")
	unstarted.Start = time.Time{}
	requeued := stressTestRecord("4", 0, 1, 2, "worker-0")
	requeued.Restarts = 1
	err := stressJobProblems([]stressJobRecord{stressTestRecord("1", 0, 1, 2, "worker-0"), failed, unstarted, requeued})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "3 of 4 jobs did not complete successfully")
	assert.Contains(t, err.Error(), "job 2 finished with state=FAILED exit_code=1:0 node=worker-1")
	assert.Contains(t, err.Error(), "job 3 lacks submit, start or end time")
	assert.Contains(t, err.Error(), "job 4 was requeued 1 times, last node=worker-0")
}

func TestStressJobProblemsTruncates(t *testing.T) {
	var records []stressJobRecord
	for i := range stressMaxJobProblems + 5 {
		record := stressTestRecord(strconv.Itoa(i), 0, 1, 2, "worker-0")
		record.State = "TIMEOUT"
		records = append(records, record)
	}
	err := stressJobProblems(records)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "job 0 finished with state=TIMEOUT")
	assert.NotContains(t, err.Error(), "job 20 finished")
	assert.Contains(t, err.Error(), "... 25 problems in total")
}

func TestComputeStressJobStats(t *testing.T) {
	records := []stressJobRecord{
		stressTestRecordWithSteps("1", 100, 101, 102, 104, 105, 107, "worker-0"),
		stressTestRecordWithSteps("2", 100, 103, 103, 110, 110, 0, "worker-1"),
		stressTestRecordWithSteps("3", 101, 110, 112, 118, 119, 119, "worker-0"),
		stressTestRecord("4", 102, 102, 120, "worker-1"),
	}
	stats := computeStressJobStats(records)
	assert.Equal(t, 4, stats.Jobs)
	assert.Equal(t, 20.0, stats.WallSeconds)
	assert.InDelta(t, 0.2, stats.ThroughputPerSecond, 1e-9)
	assert.Equal(t, stressDistribution{Mean: 3.25, P50: 1, P90: 9, P95: 9, P99: 9, Max: 9}, stats.QueueWait)
	assert.Equal(t, stressDistribution{Mean: 1, P50: 1, P90: 2, P95: 2, P99: 2, Max: 2}, stats.Startup, "jobs without step rows are left out of the step phases")
	assert.Equal(t, stressDistribution{Mean: 5, P50: 6, P90: 7, P95: 7, P99: 7, Max: 7}, stats.Steps)
	assert.Equal(t, stressDistribution{Mean: 1.0 + 1.0/3.0, P50: 1, P90: 3, P95: 3, P99: 3, Max: 3}, stats.Teardown, "teardown ends at the job end without an extern step")
	assert.Equal(t, stressDistribution{Mean: 9.5, P50: 7, P90: 18, P95: 18, P99: 18, Max: 18}, stats.Run)
	assert.Equal(t, []string{"worker-0", "worker-1"}, stats.Workers)
	assert.Equal(t, "mean 3.2s p50 1.0s p90 9.0s p95 9.0s p99 9.0s max 9.0s", stats.QueueWait.String())
	assert.Equal(t, "mean 3.2ms p50 1.0ms p90 9.0ms p95 9.0ms p99 9.0ms max 9.0ms", stats.QueueWait.format("ms"))
}

func TestPercentile(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	assert.Equal(t, 5.0, percentile(values, 0.50))
	assert.Equal(t, 10.0, percentile(values, 0.95))
	assert.Equal(t, 10.0, percentile(values, 0.99))
	hundred := make([]float64, 100)
	for i := range hundred {
		hundred[i] = float64(i + 1)
	}
	assert.Equal(t, 99.0, percentile(hundred, 0.99))
	assert.Equal(t, 95.0, percentile(hundred, 0.95))
	assert.Equal(t, 1.0, percentile(values, 0.01))
	assert.Equal(t, 0.0, percentile(nil, 0.5))
}

func TestEvaluateStressCompletion(t *testing.T) {
	stats := stressJobStats{Jobs: 1000, WallSeconds: 120, ThroughputPerSecond: 8.3, Workers: []string{"worker-0", "worker-1"}}
	require.NoError(t, evaluateStressCompletion(stats, stressCompletionBudget{Wall: 5 * time.Minute, MinWorkers: 2}))

	err := evaluateStressCompletion(stats, stressCompletionBudget{Wall: time.Minute, MinWorkers: 3})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1000 jobs took 120.0s from first submission to last completion, budget 1m0s")
	assert.Contains(t, err.Error(), "jobs ran on 2 workers (worker-0, worker-1), expected at least 3")
}

func TestEvaluateStressJobPhases(t *testing.T) {
	stats := stressJobStats{
		Startup:  stressDistribution{P99: 16, Max: 40},
		Steps:    stressDistribution{P99: 17, Max: 25},
		Teardown: stressDistribution{P99: 6, Max: 20},
	}
	budget := stressPhaseBudget{StartupP99: 30 * time.Second, StepsP99: 30 * time.Second, TeardownP99: 15 * time.Second}
	require.NoError(t, evaluateStressJobPhases(stats, budget), "the slowest single job does not count")

	stats.Startup.P99 = 45
	stats.Teardown.P99 = 15.5
	err := evaluateStressJobPhases(stats, budget)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "job startup p99 45.0s, budget 30s")
	assert.Contains(t, err.Error(), "job teardown p99 15.5s, budget 15s")
	assert.NotContains(t, err.Error(), "job steps")
}
