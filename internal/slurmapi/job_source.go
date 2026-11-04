package slurmapi

import (
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
