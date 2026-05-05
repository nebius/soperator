package slurmapi

import (
	"strings"
	"time"
)

type JobSource string

const (
	JobSourceController JobSource = "controller"
	JobSourceAccounting JobSource = "accounting"
)

// accountingEndTimeSkew pads the accounting query's end_time. The Slurm REST pipeline involves three potentially
// different machines: slurmrestd fills in the default end_time using its own clock, slurmctld stamps the job's
// time_end using its clock, and slurmdbd persists what slurmctld wrote. A small forward pad keeps "currently running"
// jobs from being filtered out when those clocks disagree. It is a safety constant, not a tuning knob.
const accountingEndTimeSkew = 5 * time.Minute

type ListJobsParams struct {
	// Source selects which Slurm API to query.
	Source JobSource

	// AccountingJobStates is a list of Slurm job-state strings forwarded verbatim to the accounting API's "state"
	// CSV query parameter (e.g. "RUNNING", "PENDING"). Empty/nil means no state filter.
	//
	// Important: slurmdbd applies this filter to the *historical* states a job held during the query
	// window [now - AccountingLookback, now + accountingEndTimeSkew] — sacct --state semantics. It is
	// NOT a filter on the current job state. A job that was RUNNING during the window and has since
	// completed will still be returned, with its current state field set to e.g. COMPLETED.
	AccountingJobStates []string

	// AccountingLookback sets the size of the time window queried from the accounting API:
	// [now - AccountingLookback, now + accountingEndTimeSkew].
	// Sacct overlap semantics mean a job is returned when its lifetime overlaps the window — long-running jobs
	// that started before the window are still included.
	AccountingLookback time.Duration

	// AccountingCluster scopes the accounting query to a single Slurm cluster name (the value of
	// `ClusterName` in slurm.conf, which slurmdbd records on every job). Required when slurmdbd
	// stores more than one cluster (federated setups), otherwise the query returns sibling
	// clusters' jobs as well. Empty means no scoping. Only applied when Source is "accounting".
	AccountingCluster string
}

// cleanedAccountingStates returns AccountingJobStates trimmed and with empty entries removed.
// Use this rather than the raw slice; the cleaned form is what gets serialized into the
// accounting API's `state` query (one repeated parameter per state — slurmrestd v0.0.41 rejects
// CSV values as a single unknown flag, so we send `state=A&state=B&...` instead).
func (p ListJobsParams) cleanedAccountingStates() []string {
	cleaned := make([]string, 0, len(p.AccountingJobStates))
	for _, s := range p.AccountingJobStates {
		if s = strings.TrimSpace(s); s != "" {
			cleaned = append(cleaned, s)
		}
	}
	return cleaned
}
