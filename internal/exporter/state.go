package exporter

import (
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

// metricsCollectorState holds the raw data collected from SLURM APIs
type metricsCollectorState struct {
	lastGPUSecondsUpdate    time.Time
	nodes                   []slurmapi.Node
	jobs                    []slurmapi.Job
	diag                    *api.V0041OpenapiDiagResp
	nodeNotUsableTimestamps map[string]time.Time // node_name -> timestamp when became not usable
	recentRestorations      map[string]float64   // node_name -> duration in seconds for recently restored nodes
}

// newMetricsCollectorState initializes a new metrics collector state
func newMetricsCollectorState() *metricsCollectorState {
	return &metricsCollectorState{
		lastGPUSecondsUpdate:    time.Now(),
		nodes:                   nil,
		nodeNotUsableTimestamps: make(map[string]time.Time),
		recentRestorations:      make(map[string]float64),
	}
}
