package exporter

import (
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

// restorationInfo holds information about a node restoration for MTTR tracking
type restorationInfo struct {
	duration    float64 // duration in seconds from not usable to usable
	scrapeCount int     // number of times this restoration has been scraped
}

// mttrPersistenceScrapes defines how many scrapes to persist MTTR data before cleanup.
// This ensures MTTR metrics are available across multiple scrape cycles, improving
// reliability in case of failed scrapes or temporary metrics collection issues.
// A value of 3 provides good balance between reliability and memory usage.
const mttrPersistenceScrapes = 3

// metricsCollectorState holds the raw data collected from SLURM APIs
type metricsCollectorState struct {
	lastGPUSecondsUpdate    time.Time
	nodes                   []slurmapi.Node
	jobs                    []slurmapi.Job
	diag                    *api.V0041OpenapiDiagResp
	nodeNotUsableTimestamps map[string]time.Time       // node_name -> timestamp when became not usable
	recentRestorations      map[string]restorationInfo // node_name -> restoration info for recently restored nodes
}

// newMetricsCollectorState initializes a new metrics collector state
func newMetricsCollectorState() *metricsCollectorState {
	return &metricsCollectorState{
		lastGPUSecondsUpdate:    time.Now(),
		nodes:                   nil,
		nodeNotUsableTimestamps: make(map[string]time.Time),
		recentRestorations:      make(map[string]restorationInfo),
	}
}
