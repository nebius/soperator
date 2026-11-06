package sharedsteps

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

// stressJobRecord is one stress job assembled from its top-level sacct row and its step rows.
type stressJobRecord struct {
	ID       string
	State    string
	ExitCode string
	NodeList string
	// Restarts counts requeues; a requeued job would otherwise pass for a slow one.
	Restarts int
	// Submit, Eligible, Start and End are zero when sacct reported None or Unknown.
	Submit   time.Time
	Eligible time.Time
	Start    time.Time
	End      time.Time
	// FirstStepStart and LastStepEnd bracket the numbered srun steps, falling back
	// to the batch step for a job without srun. ExternEnd is when the extern step
	// released the allocation (PrologFlags=contain); zero without an extern step.
	FirstStepStart time.Time
	LastStepEnd    time.Time
	ExternEnd      time.Time
}

// stressDistribution summarizes one measurement over all samples.
type stressDistribution struct {
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
	Max  float64 `json:"max"`
}

func (d stressDistribution) String() string {
	return d.format("s")
}

func (d stressDistribution) format(unit string) string {
	return fmt.Sprintf("mean %.1f%s p50 %.1f%s p90 %.1f%s p95 %.1f%s p99 %.1f%s max %.1f%s",
		d.Mean, unit, d.P50, unit, d.P90, unit, d.P95, unit, d.P99, unit, d.Max, unit)
}

// stressJobStats summarizes completed stress jobs.
// The phases tile a job's life between its accounting timestamps,
// the way the Soperator stress-test tool reports them;
// every phase is in seconds:
//
//	queue wait: Eligible -> Start
//	startup:    Start -> first srun step Start
//	steps:      first srun step Start -> last srun step End
//	teardown:   last srun step End -> extern step End
type stressJobStats struct {
	Jobs int `json:"jobs"`
	// WallSeconds spans the first submission to the last job end.
	WallSeconds float64 `json:"wall_seconds"`
	// ThroughputPerSecond is Jobs divided by WallSeconds.
	ThroughputPerSecond float64 `json:"throughput_per_second"`
	// QueueWait is Start minus Eligible: time waiting for resources.
	QueueWait stressDistribution `json:"queue_wait"`
	// Startup is the first srun step start minus the job start: prolog, batch
	// script preamble and step launch together (slurmctld stamps the prolog
	// inside the job start, so it cannot be separated).
	Startup stressDistribution `json:"startup"`
	// Steps spans the first step start to the last step end.
	Steps stressDistribution `json:"steps"`
	// Teardown is the extern step end minus the last step end: how long the node
	// kept holding the finished job. It ends at the job end without an extern step.
	Teardown stressDistribution `json:"teardown"`
	// Run is End minus Start of the job as a whole.
	Run stressDistribution `json:"run"`
	// Workers lists the distinct nodes the jobs ran on.
	Workers []string `json:"workers"`
}

// stressSacctFilter selects the load's accounting rows: the run name and user identify them,
// and the start time only bounds the slurmdbd scan.
// The query runs after the drain, up to the submission budget plus the drain and
// accounting timeouts (about 40 minutes) after the first job was submitted, so the window reaches back two hours.
// SLURM_TIME_FORMAT is pinned to epoch seconds, which the parser expects whatever the jail's login profile sets.
// Parsable output is used instead of --json, which ignores --format and dumps every field:
// for a 5000-job load that is ~80 MB and ~50 s of sacct versus ~1.5 MB and ~1 s.
func stressSacctFilter(jobName string) string {
	return "env SLURM_TIME_FORMAT=%s sacct --noheader --parsable2 --name=" + framework.ShellQuote(jobName) +
		" --user=" + framework.ShellQuote(stressUserName) + " --starttime=now-2hours"
}

// stressSacctCountCommand counts, in a few bytes, the load's top-level accounting rows by state, in `uniq --count` form:
//
//	4992 COMPLETED
//	   5 RUNNING
//	   3 COMPLETING
func stressSacctCountCommand(jobName string) string {
	return stressSacctFilter(jobName) + " --allocations --format=State | sort | uniq --count"
}

// stressAccountingSettled reads the count command output and reports whether every
// expected job has a terminal accounting record.
func stressAccountingSettled(output string, expected int) (bool, error) {
	counts := parseStateCounts(output)
	alive := map[string]int{}
	for state, count := range counts {
		if framework.IsJobAliveState(state) {
			alive[state] = count
		}
	}
	if records := sumCounts(counts); records < expected || len(alive) > 0 {
		return false, fmt.Errorf("sacct has %d of %d records, alive: %s", records, expected, formatStateCounts(alive))
	}
	return true, nil
}

// stressSacctCommand lists the accounting rows of the load with epoch timestamps.
// Each job yields four rows: the allocation, the batch and extern steps that
// PrologFlags=contain creates, and the `srun sleep` step; Restarts is set on the
// allocation row only:
//
//	67024|COMPLETED|0:0|0|1793817225|1793817225|1793817225|1793817253|worker-0
//	67024.batch|COMPLETED|0:0||1793817225|1793817225|1793817225|1793817253|worker-0
//	67024.extern|COMPLETED|0:0||1793817225|1793817225|1793817225|1793817253|worker-0
//	67024.0|COMPLETED|0:0||1793817237|1793817237|1793817237|1793817253|worker-0
func stressSacctCommand(jobName string) string {
	return stressSacctFilter(jobName) +
		" --format=JobID,State,ExitCode,Restarts,Submit,Eligible,Start,End,NodeList"
}

// stressStepRow is a sacct step row before it is merged into its job record.
type stressStepRow struct {
	name  string
	start time.Time
	end   time.Time
}

// parseStressJobRecords parses sacct rows into one record per job, merging the step rows into their job.
// Records keep the order of their top-level rows.
func parseStressJobRecords(output string) (records []stressJobRecord, err error) {
	rows := parsePipeRows(output)
	index := make(map[string]int, len(rows)/4)
	records = make([]stressJobRecord, 0, len(rows)/4)
	steps := map[string][]stressStepRow{}
	for _, fields := range rows {
		if len(fields) < 9 {
			return nil, fmt.Errorf("unexpected sacct row %q", strings.Join(fields, "|"))
		}
		id := fields[0]
		if jobID, stepName, isStep := strings.Cut(id, "."); isStep {
			step := stressStepRow{name: stepName}
			if step.start, err = parseStressEpoch(fields[6]); err != nil {
				return nil, fmt.Errorf("parse timestamp of step %s: %w", id, err)
			}
			if step.end, err = parseStressEpoch(fields[7]); err != nil {
				return nil, fmt.Errorf("parse timestamp of step %s: %w", id, err)
			}
			steps[jobID] = append(steps[jobID], step)
			continue
		}
		record := stressJobRecord{
			ID:       id,
			State:    fields[1],
			ExitCode: fields[2],
			NodeList: fields[8],
		}
		if record.Restarts, err = strconv.Atoi(fields[3]); err != nil {
			return nil, fmt.Errorf("parse restarts of job %s: %w", id, err)
		}
		for i, target := range []*time.Time{&record.Submit, &record.Eligible, &record.Start, &record.End} {
			if *target, err = parseStressEpoch(fields[4+i]); err != nil {
				return nil, fmt.Errorf("parse timestamp of job %s: %w", id, err)
			}
		}
		index[id] = len(records)
		records = append(records, record)
	}
	for jobID, rows := range steps {
		if i, ok := index[jobID]; ok {
			mergeStressSteps(&records[i], rows)
		}
	}
	return records, nil
}

// mergeStressSteps derives the step phase boundaries of a job from its step rows.
func mergeStressSteps(record *stressJobRecord, rows []stressStepRow) {
	var batch *stressStepRow
	for i := range rows {
		row := &rows[i]
		switch row.name {
		case "extern":
			record.ExternEnd = row.end
		case "batch":
			batch = row
		default:
			if row.start.IsZero() || row.end.IsZero() {
				continue
			}
			if record.FirstStepStart.IsZero() || row.start.Before(record.FirstStepStart) {
				record.FirstStepStart = row.start
			}
			if row.end.After(record.LastStepEnd) {
				record.LastStepEnd = row.end
			}
		}
	}
	if record.FirstStepStart.IsZero() && batch != nil {
		record.FirstStepStart = batch.start
		record.LastStepEnd = batch.end
	}
}

func parseStressEpoch(value string) (time.Time, error) {
	switch value {
	case "", "None", "Unknown":
		return time.Time{}, nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse epoch %q: %w", value, err)
	}
	return time.Unix(seconds, 0), nil
}

// stressMaxJobProblems bounds how many failed jobs a failure message quotes.
const stressMaxJobProblems = 20

// stressJobProblem describes why a job did not complete cleanly, or is empty for a clean one.
func stressJobProblem(record stressJobRecord) string {
	if record.State != "COMPLETED" || record.ExitCode != "0:0" {
		return fmt.Sprintf("job %s finished with state=%s exit_code=%s node=%s",
			record.ID, record.State, record.ExitCode, record.NodeList)
	}
	if record.Restarts > 0 {
		return fmt.Sprintf("job %s was requeued %d times, last node=%s", record.ID, record.Restarts, record.NodeList)
	}
	if record.Submit.IsZero() || record.Start.IsZero() || record.End.IsZero() {
		return fmt.Sprintf("job %s lacks submit, start or end time", record.ID)
	}
	return ""
}

// stressJobProblems reports jobs that did not complete cleanly, quoting the first few in sacct order.
func stressJobProblems(records []stressJobRecord) error {
	var problems []string
	for _, record := range records {
		if problem := stressJobProblem(record); problem != "" {
			problems = append(problems, problem)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	message := strings.Join(problems[:min(len(problems), stressMaxJobProblems)], "; ")
	if len(problems) > stressMaxJobProblems {
		message += fmt.Sprintf("; ... %d problems in total", len(problems))
	}
	return fmt.Errorf("%d of %d jobs did not complete successfully: %s", len(problems), len(records), message)
}

// computeStressJobStats summarizes records that stressJobProblems has already accepted.
func computeStressJobStats(records []stressJobRecord) stressJobStats {
	var firstSubmit, lastEnd time.Time
	waits := make([]float64, 0, len(records))
	runs := make([]float64, 0, len(records))
	startups := make([]float64, 0, len(records))
	steps := make([]float64, 0, len(records))
	teardowns := make([]float64, 0, len(records))
	workers := map[string]struct{}{}
	for _, record := range records {
		if firstSubmit.IsZero() || record.Submit.Before(firstSubmit) {
			firstSubmit = record.Submit
		}
		if record.End.After(lastEnd) {
			lastEnd = record.End
		}
		eligible := record.Eligible
		if eligible.IsZero() {
			eligible = record.Submit
		}
		waits = append(waits, secondsBetween(eligible, record.Start))
		runs = append(runs, secondsBetween(record.Start, record.End))
		if !record.FirstStepStart.IsZero() && !record.LastStepEnd.IsZero() {
			startups = append(startups, secondsBetween(record.Start, record.FirstStepStart))
			steps = append(steps, secondsBetween(record.FirstStepStart, record.LastStepEnd))
			released := record.ExternEnd
			if released.IsZero() {
				released = record.End
			}
			teardowns = append(teardowns, secondsBetween(record.LastStepEnd, released))
		}
		if record.NodeList != "" {
			workers[record.NodeList] = struct{}{}
		}
	}
	stats := stressJobStats{
		Jobs:        len(records),
		WallSeconds: math.Max(lastEnd.Sub(firstSubmit).Seconds(), 1),
		QueueWait:   summarizeStressDistribution(waits),
		Startup:     summarizeStressDistribution(startups),
		Steps:       summarizeStressDistribution(steps),
		Teardown:    summarizeStressDistribution(teardowns),
		Run:         summarizeStressDistribution(runs),
	}
	stats.ThroughputPerSecond = float64(stats.Jobs) / stats.WallSeconds
	for worker := range workers {
		stats.Workers = append(stats.Workers, worker)
	}
	sort.Strings(stats.Workers)
	return stats
}

// secondsBetween returns `to` minus `from` in seconds, never negative: sacct stamps
// have one-second resolution and some phases are stamped with the same instant.
func secondsBetween(from, to time.Time) float64 {
	return math.Max(0, to.Sub(from).Seconds())
}

// summarizeStressDistribution sorts values in place and summarizes them.
func summarizeStressDistribution(values []float64) stressDistribution {
	if len(values) == 0 {
		return stressDistribution{}
	}
	sort.Float64s(values)
	var sum float64
	for _, value := range values {
		sum += value
	}
	return stressDistribution{
		Mean: sum / float64(len(values)),
		P50:  percentile(values, 0.50),
		P90:  percentile(values, 0.90),
		P95:  percentile(values, 0.95),
		P99:  percentile(values, 0.99),
		Max:  values[len(values)-1],
	}
}

// percentile returns the nearest-rank percentile of sorted ascending values.
func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(q*float64(len(sorted)))) - 1
	return sorted[max(0, min(rank, len(sorted)-1))]
}

type stressCompletionBudget struct {
	// Wall bounds first submission to last job end; queue wait needs no budget of
	// its own because it is the same drain seen from the job's side.
	Wall time.Duration
	// MinWorkers is the number of distinct workers the jobs must have used, so
	// that a reserved worker that ran nothing does not pass as a slow but valid run.
	MinWorkers int
}

func evaluateStressCompletion(stats stressJobStats, budget stressCompletionBudget) error {
	var problems []string
	if stats.WallSeconds > budget.Wall.Seconds() {
		problems = append(problems, fmt.Sprintf("%d jobs took %.1fs from first submission to last completion, budget %s",
			stats.Jobs, stats.WallSeconds, budget.Wall))
	}
	if len(stats.Workers) < budget.MinWorkers {
		problems = append(problems, fmt.Sprintf("jobs ran on %d workers (%s), expected at least %d",
			len(stats.Workers), strings.Join(stats.Workers, ", "), budget.MinWorkers))
	}
	return joinProblems(problems)
}

// stressPhaseBudget bounds the node-side phases of a job at p99, where a prolog,
// epilog, slurmd or jail regression shows before it moves the wall time; a single
// slow job out of thousands is noise at that level.
type stressPhaseBudget struct {
	StartupP99  time.Duration
	StepsP99    time.Duration
	TeardownP99 time.Duration
}

func evaluateStressJobPhases(stats stressJobStats, budget stressPhaseBudget) error {
	var problems []string
	for _, phase := range []struct {
		name   string
		p99    float64
		budget time.Duration
	}{
		{"startup", stats.Startup.P99, budget.StartupP99},
		{"steps", stats.Steps.P99, budget.StepsP99},
		{"teardown", stats.Teardown.P99, budget.TeardownP99},
	} {
		if phase.p99 > phase.budget.Seconds() {
			problems = append(problems, fmt.Sprintf("job %s p99 %.1fs, budget %s", phase.name, phase.p99, phase.budget))
		}
	}
	return joinProblems(problems)
}
