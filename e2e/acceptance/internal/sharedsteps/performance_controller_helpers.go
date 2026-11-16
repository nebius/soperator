package sharedsteps

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// stressSample is one controller responsiveness probe taken while the load runs.
type stressSample struct {
	At           time.Time `json:"at"`
	SqueueMillis int64     `json:"squeue_ms"`
	SinfoMillis  int64     `json:"sinfo_ms"`
	SdiagMillis  int64     `json:"sdiag_ms"`
	// Pending and Running are the queue occupancy squeue reports.
	Pending int `json:"pending"`
	Running int `json:"running"`
	// Sdiag is the controller's statistics at the probe, nil when sdiag failed.
	Sdiag *sdiagStatistics `json:"sdiag,omitempty"`
	// Error is set when the probe could not be measured: the exec failed or its output is incomplete.
	// Such a sample says nothing about the controller and is left out of the statistics.
	Error string `json:"error,omitempty"`
	// SlurmError names the Slurm commands that exited nonzero, with their status and how long they ran.
	// The controller answered badly or not within MessageTimeout, so the latencies are real observations
	// and stay in the statistics, and one such sample fails the responsiveness check.
	SlurmError string `json:"slurm_error,omitempty"`
}

// sdiagStatistics is the subset of `sdiag --json` statistics the scenario evaluates:
// gauges of the moment first, then counters that only grow since the last counter reset.
type sdiagStatistics struct {
	ServerThreadCount int64 `json:"server_thread_count"`
	AgentQueueSize    int64 `json:"agent_queue_size"`

	// ScheduleCycleSum and BackfillCycleSum are the total main and backfill scheduler times in microseconds.
	ScheduleCycleSum     int64 `json:"schedule_cycle_sum"`
	ScheduleCycleTotal   int64 `json:"schedule_cycle_total"`
	BackfillCycleSum     int64 `json:"bf_cycle_sum"`
	BackfillCycleCounter int64 `json:"bf_cycle_counter"`
	JobsSubmitted        int64 `json:"jobs_submitted"`
	JobsCanceled         int64 `json:"jobs_canceled"`
	JobsFailed           int64 `json:"jobs_failed"`
}

// counters lists the cumulative statistics, the ones a reset sends backwards.
func (s sdiagStatistics) counters() [7]int64 {
	return [7]int64{s.ScheduleCycleSum, s.ScheduleCycleTotal, s.BackfillCycleSum, s.BackfillCycleCounter,
		s.JobsSubmitted, s.JobsCanceled, s.JobsFailed}
}

// wentBackwards reports whether any counter is lower in to than in from, which only a reset does.
func (s sdiagStatistics) wentBackwards(to sdiagStatistics) bool {
	fromCounters, toCounters := s.counters(), to.counters()
	for i := range fromCounters {
		if toCounters[i] < fromCounters[i] {
			return true
		}
	}
	return false
}

// stressSchedulerSummary is the controller's own view of the load, summed over the
// sdiag snapshots taken before, during and after it.
type stressSchedulerSummary struct {
	// Resets counts the counter resets seen between consecutive snapshots; each costs
	// at most one probe interval of counters, which makes JobsSubmitted a lower bound.
	Resets        int   `json:"resets"`
	JobsSubmitted int64 `json:"jobs_submitted"`
	// JobsFailed and JobsCanceled count every user's jobs, so they are reported, not
	// asserted: the load's own jobs are checked one by one from accounting.
	JobsFailed            int64   `json:"jobs_failed"`
	JobsCanceled          int64   `json:"jobs_canceled"`
	ScheduleCycles        int64   `json:"schedule_cycles"`
	ScheduleCycleMeanSecs float64 `json:"schedule_cycle_mean_seconds"`
	BackfillCycles        int64   `json:"backfill_cycles"`
	BackfillCycleMeanSecs float64 `json:"backfill_cycle_mean_seconds"`
	AgentQueueSize        int64   `json:"agent_queue_size"`
	ServerThreadCount     int64   `json:"server_thread_count"`
}

// stressProbeSummary aggregates the responsiveness probes; latencies are in milliseconds.
type stressProbeSummary struct {
	Samples int `json:"samples"`

	// Errors counts the probes that could not be measured.
	Errors int `json:"errors"`
	// SlurmErrors counts the probes whose Slurm command exited nonzero.
	SlurmErrors int `json:"slurm_errors"`
	// FirstSlurmError describes the earliest of those probes, prefixed with its time.
	FirstSlurmError string `json:"first_slurm_error,omitempty"`

	Squeue     stressDistribution `json:"squeue_ms"`
	Sinfo      stressDistribution `json:"sinfo_ms"`
	Sdiag      stressDistribution `json:"sdiag_ms"`
	PendingMax int                `json:"pending_max"`
	RunningMax int                `json:"running_max"`
}

// stressProbeCommand times squeue, sinfo and sdiag inside the jail, so the sample excludes
// kubectl exec overhead, and reports the queue occupancy by state and the controller statistics.
// The three commands run concurrently so that a controller stall is seen by all of them alike;
// run one after another, the first one absorbs the stall and the others look fast.
// It is a single line because the runner echoes every command it runs.
func stressProbeCommand() string {
	timed := func(name, command string) string {
		return "( started=$(date +%s%N); " + command + "; status=$?; finished=$(date +%s%N)" +
			"; echo " + name + "_ms=$(( (finished - started) / 1000000 )); echo " + name + "_status=$status ) > \"$dir/" + name + ".timing\" &"
	}
	// The background groups end in "&", which already terminates a command: no "; " after them.
	probes := strings.Join([]string{
		timed("squeue", "squeue --noheader --states=PD,R --format='%T' | sort | uniq --count > \"$dir/states\""),
		timed("sinfo", "sinfo --noheader >/dev/null"),
		timed("sdiag", "sdiag --json > \"$dir/sdiag\""),
		"wait",
	}, " ")
	return strings.Join([]string{
		"set -o pipefail",
		"dir=$(mktemp -d)",
		probes,
		"cat \"$dir/squeue.timing\" \"$dir/sinfo.timing\" \"$dir/sdiag.timing\"",
		"echo " + stressProbeStatesMarker,
		"cat \"$dir/states\"",
		"echo " + stressProbeSdiagMarker,
		"cat \"$dir/sdiag\"",
		"rm -rf \"$dir\"",
	}, "; ")
}

// The markers separate the probe's key=value lines from the state counts and the sdiag JSON.
const (
	stressProbeStatesMarker = "states:"
	stressProbeSdiagMarker  = "sdiag:"
)

func parseStressSample(output string) stressSample {
	header, rest, foundStates := strings.Cut(output, stressProbeStatesMarker+"\n")
	states, sdiag, foundSdiag := strings.Cut(rest, stressProbeSdiagMarker+"\n")
	values := parseKeyValues(header)
	var sample stressSample
	var problems []string
	if !foundStates {
		problems = append(problems, "no "+stressProbeStatesMarker+" section")
	}
	if !foundSdiag {
		problems = append(problems, "no "+stressProbeSdiagMarker+" section")
	}
	millis := func(key string) int64 {
		parsed, err := strconv.ParseInt(values[key], 10, 64)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s=%q", key, values[key]))
		}
		return parsed
	}
	sample.SqueueMillis = millis("squeue_ms")
	sample.SinfoMillis = millis("sinfo_ms")
	sample.SdiagMillis = millis("sdiag_ms")
	var failed []string
	for _, command := range []struct {
		name   string
		millis int64
	}{{"squeue", sample.SqueueMillis}, {"sinfo", sample.SinfoMillis}, {"sdiag", sample.SdiagMillis}} {
		status, ok := values[command.name+"_status"]
		switch {
		case !ok:
			problems = append(problems, command.name+"_status missing")
		case status != "0":
			failed = append(failed, fmt.Sprintf("%s exited %s after %d ms", command.name, status, command.millis))
		}
	}
	sample.SlurmError = strings.Join(failed, ", ")
	counts := parseStateCounts(states)
	sample.Pending = counts["PENDING"]
	sample.Running = counts["RUNNING"]
	if foundSdiag {
		if statistics, err := parseSdiag(sdiag); err != nil {
			problems = append(problems, err.Error())
		} else {
			sample.Sdiag = &statistics
		}
	}
	if len(problems) > 0 {
		sample.Error = "probe output: " + strings.Join(problems, ", ")
	}
	return sample
}

func summarizeStressProbes(samples []stressSample) stressProbeSummary {
	summary := stressProbeSummary{Samples: len(samples)}
	squeue := make([]float64, 0, len(samples))
	sinfo := make([]float64, 0, len(samples))
	sdiag := make([]float64, 0, len(samples))
	for _, sample := range samples {
		if sample.Error != "" {
			summary.Errors++
			continue
		}
		if sample.SlurmError != "" {
			summary.SlurmErrors++
			if summary.FirstSlurmError == "" {
				summary.FirstSlurmError = sample.At.Format(time.TimeOnly) + ": " + sample.SlurmError
			}
		}
		squeue = append(squeue, float64(sample.SqueueMillis))
		sinfo = append(sinfo, float64(sample.SinfoMillis))
		sdiag = append(sdiag, float64(sample.SdiagMillis))
		summary.PendingMax = max(summary.PendingMax, sample.Pending)
		summary.RunningMax = max(summary.RunningMax, sample.Running)
	}
	summary.Squeue = summarizeStressDistribution(squeue)
	summary.Sinfo = summarizeStressDistribution(sinfo)
	summary.Sdiag = summarizeStressDistribution(sdiag)
	return summary
}

// evaluateStressProbes fails on measured latency only:
// a probe that could not run at all (for example a kubectl exec that lost its connection) says nothing about
// the controller and is just left out, as long as enough probes did run.
type stressProbeBudget struct {
	// SqueueMean and SinfoMean bound the typical latency, which grows with the queue depth.
	// They are means rather than percentiles because the periodic scheduler pass stalls
	// whichever probe it meets, so the upper percentiles depend on the probe phase.
	SqueueMean time.Duration
	SinfoMean  time.Duration
	// SqueueMax, SinfoMax and SdiagMax bound every single probe of that command.
	SqueueMax time.Duration
	SinfoMax  time.Duration
	SdiagMax  time.Duration
}

func evaluateStressProbes(summary stressProbeSummary, budget stressProbeBudget) error {
	const stressMinProbeSamples = 3
	if measured := summary.Samples - summary.Errors; measured < stressMinProbeSamples {
		return fmt.Errorf("only %d of %d responsiveness probes ran, expected at least %d", measured, summary.Samples, stressMinProbeSamples)
	}
	// A Slurm command that fails during the load is never within budget: the client gave up
	// after MessageTimeout or the controller refused it, whatever the latency statistics say.
	if summary.SlurmErrors > 0 {
		return fmt.Errorf("controller failed %d of %d responsiveness probes during the load, first at %s",
			summary.SlurmErrors, summary.Samples, summary.FirstSlurmError)
	}
	var problems []string
	for _, probe := range []struct {
		name      string
		latency   stressDistribution
		mean, max time.Duration
	}{
		{"squeue", summary.Squeue, budget.SqueueMean, budget.SqueueMax},
		{"sinfo", summary.Sinfo, budget.SinfoMean, budget.SinfoMax},
		{"sdiag", summary.Sdiag, 0, budget.SdiagMax},
	} {
		if probe.mean > 0 && probe.latency.Mean > float64(probe.mean.Milliseconds()) {
			problems = append(problems, fmt.Sprintf("%s mean %.0f ms, budget %s", probe.name, probe.latency.Mean, probe.mean))
		}
		if probe.latency.Max > float64(probe.max.Milliseconds()) {
			problems = append(problems, fmt.Sprintf("%s max %.0f ms, budget %s", probe.name, probe.latency.Max, probe.max))
		}
	}
	if err := joinProblems(problems); err != nil {
		return fmt.Errorf("controller was not responsive during the load: %w", err)
	}
	return nil
}

func parseSdiag(output string) (sdiagStatistics, error) {
	var snapshot struct {
		Statistics sdiagStatistics `json:"statistics"`
	}
	if err := json.Unmarshal([]byte(output), &snapshot); err != nil {
		return sdiagStatistics{}, fmt.Errorf("parse sdiag JSON: %w", err)
	}
	return snapshot.Statistics, nil
}

// summarizeStressScheduler derives the load's share of the controller statistics from snapshots in time order,
// the first taken before the load and the last after it.
// The counters live in slurmctld memory and restart from zero whenever it resets them:
// daily at midnight, on `sdiag --reset`, and when the daemon restarts or re-executes on reconfigure.
// So the deltas are summed interval by interval: counters going backwards within an interval mean a reset,
// and that interval contributes the absolutes accumulated since the reset instead of a delta.
func summarizeStressScheduler(snapshots []sdiagStatistics) stressSchedulerSummary {
	last := snapshots[len(snapshots)-1]
	summary := stressSchedulerSummary{
		AgentQueueSize:    last.AgentQueueSize,
		ServerThreadCount: last.ServerThreadCount,
	}
	var cycleSum, backfillSum int64
	for i := 1; i < len(snapshots); i++ {
		from, to := snapshots[i-1], snapshots[i]
		if from.wentBackwards(to) {
			summary.Resets++
			from = sdiagStatistics{}
		}
		summary.JobsSubmitted += to.JobsSubmitted - from.JobsSubmitted
		summary.JobsFailed += to.JobsFailed - from.JobsFailed
		summary.JobsCanceled += to.JobsCanceled - from.JobsCanceled
		summary.ScheduleCycles += to.ScheduleCycleTotal - from.ScheduleCycleTotal
		cycleSum += to.ScheduleCycleSum - from.ScheduleCycleSum
		summary.BackfillCycles += to.BackfillCycleCounter - from.BackfillCycleCounter
		backfillSum += to.BackfillCycleSum - from.BackfillCycleSum
	}
	if summary.ScheduleCycles > 0 {
		summary.ScheduleCycleMeanSecs = float64(cycleSum) / float64(summary.ScheduleCycles) / 1e6
	}
	if summary.BackfillCycles > 0 {
		summary.BackfillCycleMeanSecs = float64(backfillSum) / float64(summary.BackfillCycles) / 1e6
	}
	return summary
}

type stressSchedulerBudget struct {
	MinSubmitted      int64
	ScheduleCycleMean time.Duration
	BackfillCycleMean time.Duration
	MaxServerThreads  int64
}

func evaluateStressScheduler(summary stressSchedulerSummary, budget stressSchedulerBudget) error {
	var problems []string
	// The counters reset at midnight on the controller; a reset inside the submission phase
	// loses up to one probe interval of submissions, so the count is only a lower bound then.
	// Accounting has already proven every job individually by this step, so the sdiag count
	// is only asserted when it is exact.
	if summary.Resets == 0 && summary.JobsSubmitted < budget.MinSubmitted {
		problems = append(problems, fmt.Sprintf("controller counted %d submitted jobs, expected at least %d", summary.JobsSubmitted, budget.MinSubmitted))
	}
	for _, scheduler := range []struct {
		name     string
		cycles   int64
		meanSecs float64
		budget   time.Duration
	}{
		{"main", summary.ScheduleCycles, summary.ScheduleCycleMeanSecs, budget.ScheduleCycleMean},
		{"backfill", summary.BackfillCycles, summary.BackfillCycleMeanSecs, budget.BackfillCycleMean},
	} {
		if scheduler.cycles == 0 {
			problems = append(problems, fmt.Sprintf("controller recorded no %s scheduler cycles during the load", scheduler.name))
		} else if scheduler.meanSecs > scheduler.budget.Seconds() {
			problems = append(problems, fmt.Sprintf("%s scheduler mean cycle %.3fs over %d cycles exceeds budget %s",
				scheduler.name, scheduler.meanSecs, scheduler.cycles, scheduler.budget))
		}
	}
	if summary.AgentQueueSize > 0 {
		problems = append(problems, fmt.Sprintf("controller agent queue still holds %d requests", summary.AgentQueueSize))
	}
	if summary.ServerThreadCount > budget.MaxServerThreads {
		problems = append(problems, fmt.Sprintf("controller still runs %d server threads, expected at most %d", summary.ServerThreadCount, budget.MaxServerThreads))
	}
	return joinProblems(problems)
}
