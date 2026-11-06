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
	Error string           `json:"error,omitempty"`
}

// sdiagStatistics is the subset of `sdiag --json` statistics the scenario evaluates:
// gauges of the moment first, then counters that only grow since the last counter reset.
type sdiagStatistics struct {
	ServerThreadCount int64 `json:"server_thread_count"`
	AgentQueueSize    int64 `json:"agent_queue_size"`

	// ScheduleCycleSum is the total main scheduler time in microseconds.
	ScheduleCycleSum   int64 `json:"schedule_cycle_sum"`
	ScheduleCycleTotal int64 `json:"schedule_cycle_total"`
	JobsSubmitted      int64 `json:"jobs_submitted"`
	JobsCanceled       int64 `json:"jobs_canceled"`
	JobsFailed         int64 `json:"jobs_failed"`
}

// counters lists the cumulative statistics, the ones a reset sends backwards.
func (s sdiagStatistics) counters() [5]int64 {
	return [5]int64{s.ScheduleCycleSum, s.ScheduleCycleTotal, s.JobsSubmitted, s.JobsCanceled, s.JobsFailed}
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
	// at most one probe interval of counters.
	Resets        int   `json:"resets"`
	JobsSubmitted int64 `json:"jobs_submitted"`
	// JobsFailed and JobsCanceled count every user's jobs, so they are reported, not
	// asserted: the load's own jobs are checked one by one from accounting.
	JobsFailed            int64   `json:"jobs_failed"`
	JobsCanceled          int64   `json:"jobs_canceled"`
	ScheduleCycles        int64   `json:"schedule_cycles"`
	ScheduleCycleMeanSecs float64 `json:"schedule_cycle_mean_seconds"`
	AgentQueueSize        int64   `json:"agent_queue_size"`
	ServerThreadCount     int64   `json:"server_thread_count"`
}

// stressProbeSummary aggregates the responsiveness probes; latencies are in milliseconds.
type stressProbeSummary struct {
	Samples    int                `json:"samples"`
	Errors     int                `json:"errors"`
	Squeue     stressDistribution `json:"squeue_ms"`
	Sinfo      stressDistribution `json:"sinfo_ms"`
	Sdiag      stressDistribution `json:"sdiag_ms"`
	PendingMax int                `json:"pending_max"`
	RunningMax int                `json:"running_max"`
}

// stressProbeCommand times squeue, sinfo and sdiag inside the jail, so the sample excludes
// kubectl exec overhead, and reports the queue occupancy by state and the controller statistics.
// It is a single line because the runner echoes every command it runs.
func stressProbeCommand() string {
	return strings.Join([]string{
		"set -o pipefail",
		"started=$(date +%s%N); states=$(squeue --noheader --states=PD,R --format='%T' | sort | uniq --count); squeue_status=$?; finished=$(date +%s%N)",
		"echo squeue_ms=$(( (finished - started) / 1000000 ))",
		"echo squeue_status=$squeue_status",
		"started=$(date +%s%N); sinfo --noheader >/dev/null; sinfo_status=$?; finished=$(date +%s%N)",
		"echo sinfo_ms=$(( (finished - started) / 1000000 ))",
		"echo sinfo_status=$sinfo_status",
		"started=$(date +%s%N); sdiag=$(sdiag --json); sdiag_status=$?; finished=$(date +%s%N)",
		"echo sdiag_ms=$(( (finished - started) / 1000000 ))",
		"echo sdiag_status=$sdiag_status",
		"echo " + stressProbeStatesMarker,
		"echo \"$states\"",
		"echo " + stressProbeSdiagMarker,
		"echo \"$sdiag\"",
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
	for _, key := range []string{"squeue_status", "sinfo_status", "sdiag_status"} {
		if values[key] != "0" {
			problems = append(problems, fmt.Sprintf("%s=%q", key, values[key]))
		}
	}
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
	// SqueueMean bounds the typical squeue latency, which grows with the queue depth.
	// It is a mean rather than a percentile because the periodic scheduler pass stalls
	// whichever probe it meets, so the upper percentiles depend on the probe phase.
	// sinfo has no typical-latency budget for the same reason.
	SqueueMean time.Duration
	// SqueueMax, SinfoMax and SdiagMax bound every single probe of that command.
	SqueueMax time.Duration
	SinfoMax  time.Duration
	SdiagMax  time.Duration
	// MinSamples is the least number of probes that must have run for a verdict.
	MinSamples int
}

func evaluateStressProbes(summary stressProbeSummary, budget stressProbeBudget) error {
	if measured := summary.Samples - summary.Errors; measured < budget.MinSamples {
		return fmt.Errorf("only %d of %d responsiveness probes ran, expected at least %d", measured, summary.Samples, budget.MinSamples)
	}
	var problems []string
	if summary.Squeue.Mean > float64(budget.SqueueMean.Milliseconds()) {
		problems = append(problems, fmt.Sprintf("squeue mean %.0f ms, budget %s", summary.Squeue.Mean, budget.SqueueMean))
	}
	for _, probe := range []struct {
		name    string
		latency stressDistribution
		max     time.Duration
	}{
		{"squeue", summary.Squeue, budget.SqueueMax},
		{"sinfo", summary.Sinfo, budget.SinfoMax},
		{"sdiag", summary.Sdiag, budget.SdiagMax},
	} {
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
	var cycleSum int64
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
	}
	if summary.ScheduleCycles > 0 {
		summary.ScheduleCycleMeanSecs = float64(cycleSum) / float64(summary.ScheduleCycles) / 1e6
	}
	return summary
}

type stressSchedulerBudget struct {
	MinSubmitted      int64
	ScheduleCycleMean time.Duration
	MaxServerThreads  int64
}

func evaluateStressScheduler(summary stressSchedulerSummary, budget stressSchedulerBudget) error {
	var problems []string
	if summary.JobsSubmitted < budget.MinSubmitted {
		problems = append(problems, fmt.Sprintf("controller counted %d submitted jobs, expected at least %d", summary.JobsSubmitted, budget.MinSubmitted))
	}
	if summary.ScheduleCycles == 0 {
		problems = append(problems, "controller recorded no main scheduler cycles during the load")
	} else if summary.ScheduleCycleMeanSecs > budget.ScheduleCycleMean.Seconds() {
		problems = append(problems, fmt.Sprintf("main scheduler mean cycle %.3fs over %d cycles exceeds budget %s",
			summary.ScheduleCycleMeanSecs, summary.ScheduleCycles, budget.ScheduleCycleMean))
	}
	if summary.AgentQueueSize > 0 {
		problems = append(problems, fmt.Sprintf("controller agent queue still holds %d requests", summary.AgentQueueSize))
	}
	if summary.ServerThreadCount > budget.MaxServerThreads {
		problems = append(problems, fmt.Sprintf("controller still runs %d server threads, expected at most %d", summary.ServerThreadCount, budget.MaxServerThreads))
	}
	return joinProblems(problems)
}
