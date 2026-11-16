package sharedsteps

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func errOnly(_ []stressBudgetRow, err error) error {
	return err
}

func TestStressBudgetRows(t *testing.T) {
	rows, err := evaluateStressSubmit(stressSubmitResult{StartEpoch: 0, EndEpoch: 272.4366, Accepted: 5000}, 5000, 6*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, []stressBudgetRow{{Name: "all jobs accepted", Observed: "4m32.437s", Limit: "6m0s", Used: 272.4366 / 360}}, rows)

	rows, err = evaluateStressCompletion(stressJobStats{Jobs: 5000, WallSeconds: 1260, Workers: []string{"worker-0"}},
		stressCompletionBudget{Wall: 20 * time.Minute, MinWorkers: 1})
	require.Error(t, err)
	assert.Equal(t, []stressBudgetRow{{Name: "all jobs completed", Observed: "21m0s", Limit: "20m0s", Used: 1.05, Failed: true}}, rows)

	rows, err = evaluateStressJobPhases(stressJobStats{
		Startup:  stressDistribution{P99: 2},
		Steps:    stressDistribution{P99: 12},
		Teardown: stressDistribution{P99: 10},
	}, stressPhaseBudget{StartupP99: 10 * time.Second, StepsP99: 20 * time.Second, TeardownP99: 10 * time.Second})
	require.NoError(t, err, "a value equal to its limit is within budget")
	assert.Equal(t, []stressBudgetRow{
		{Name: "startup p99", Observed: "2s", Limit: "10s", Used: 0.2},
		{Name: "steps p99", Observed: "12s", Limit: "20s", Used: 0.6},
		{Name: "teardown p99", Observed: "10s", Limit: "10s", Used: 1},
	}, rows)

	probeBudget := stressProbeBudget{SqueueMean: time.Second, SinfoMean: time.Second, SqueueMax: 10 * time.Second, SinfoMax: 10 * time.Second, SdiagMax: 10 * time.Second}
	probes := stressProbeSummary{
		Samples: 86,
		Squeue:  stressDistribution{Mean: 355, Max: 10858},
		Sinfo:   stressDistribution{Mean: 317, Max: 500},
		Sdiag:   stressDistribution{Mean: 388, Max: 6656},
	}
	rows, err = evaluateStressProbes(probes, probeBudget)
	require.EqualError(t, err, "controller was not responsive during the load: squeue max 10858 ms, budget 10s")
	assert.Equal(t, []stressBudgetRow{
		{Name: "squeue mean", Observed: "355ms", Limit: "1s", Used: 0.355},
		{Name: "sinfo mean", Observed: "317ms", Limit: "1s", Used: 0.317},
		{Name: "squeue max", Observed: "10.858s", Limit: "10s", Used: 1.0858, Failed: true},
		{Name: "sinfo max", Observed: "500ms", Limit: "10s", Used: 0.05},
		{Name: "sdiag max", Observed: "6.656s", Limit: "10s", Used: 0.6656},
	}, rows)

	rows, err = evaluateStressProbes(stressProbeSummary{Samples: 2, Squeue: stressDistribution{Max: 20000}}, probeBudget)
	require.ErrorContains(t, err, "only 2 of 2 responsiveness probes ran")
	for _, row := range rows {
		assert.Equal(t, stressBudgetRow{Name: row.Name, Observed: "-", Limit: row.Limit}, row, "too few probes measure nothing")
	}

	schedulerBudget := stressSchedulerBudget{ScheduleCycleMean: 200 * time.Millisecond, BackfillCycleMean: 200 * time.Millisecond, MaxServerThreads: 5}
	rows, err = evaluateStressScheduler(stressSchedulerSummary{ScheduleCycles: 669, ScheduleCycleMeanSecs: 0.034, ServerThreadCount: 7}, schedulerBudget)
	require.ErrorContains(t, err, "recorded no backfill scheduler cycles")
	require.ErrorContains(t, err, "still runs 7 server threads")
	assert.Equal(t, []stressBudgetRow{
		{Name: "main scheduler cycle mean", Observed: "34ms", Limit: "200ms", Used: 0.17},
		{Name: "backfill scheduler cycle mean", Observed: "-", Limit: "200ms"},
		{Name: "server threads at the end", Observed: "7", Limit: "5", Used: 1.4, Failed: true},
	}, rows)
}

func TestStressBudgetReport(t *testing.T) {
	var report stressBudgetReport
	report.add([]stressBudgetRow{{Name: "all jobs accepted", Observed: "4m32s", Limit: "6m0s", Used: 0.76}}, true, nil)
	report.problem(nil)
	report.problem(errors.New("wait for jobs to leave the queue: timed out"))
	report.add([]stressBudgetRow{{Name: "all jobs completed", Observed: "0s", Limit: "20m0s"}}, false, errors.New("jobs ran on 0 workers"))
	report.add([]stressBudgetRow{{Name: "squeue max", Observed: "10.858s", Limit: "10s", Used: 1.0858, Failed: true}},
		true, errors.New("squeue max 10858 ms, budget 10s"))

	assert.Equal(t, []stressBudgetRow{
		{Name: "all jobs accepted", Observed: "4m32s", Limit: "6m0s", Used: 0.76},
		{Name: "all jobs completed", Observed: "-", Limit: "20m0s"},
		{Name: "squeue max", Observed: "10.858s", Limit: "10s", Used: 1.0858, Failed: true},
	}, report.rows)
	assert.Equal(t, []string{"wait for jobs to leave the queue: timed out", "squeue max 10858 ms, budget 10s"}, report.problems,
		"a failed measurement replaces the budget verdict computed from its empty values")
}

func TestFormatStressBudgets(t *testing.T) {
	table := formatStressBudgets([]stressBudgetRow{
		{Name: "all jobs completed", Observed: "14m5s", Limit: "20m0s", Used: 0.704},
		{Name: "steps p99", Observed: "15.9s", Limit: "20s", Used: 0.795},
		{Name: "teardown p99", Observed: "8s", Limit: "10s", Used: 0.8},
		{Name: "squeue max", Observed: "10.858s", Limit: "10s", Used: 1.0858, Failed: true},
		{Name: "main scheduler cycle mean", Observed: "-", Limit: "200ms"},
	})
	assert.Equal(t, `  BUDGET                     OBSERVED  LIMIT  USED
  all jobs completed         14m5s     20m0s   70%
  steps p99                  15.9s     20s     80%
  teardown p99               8s        10s     80%  near
  squeue max                 10.858s   10s    109%  FAIL
  main scheduler cycle mean  -         200ms  -`, table)
}
