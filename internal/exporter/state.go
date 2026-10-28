package exporter

import (
	"time"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

// metricsCollectorState holds the raw data collected from SLURM APIs
type metricsCollectorState struct {
	lastGPUSecondsUpdate         time.Time
	nodes                        []slurmapi.Node
	jobs                         []slurmapi.Job
	diag                         *slurmapi.Diag
	nodeUnavailabilityStartTimes map[string]time.Time
	nodeDrainingStartTimes       map[string]time.Time
}

// newMetricsCollectorState initializes a new metrics collector state
func newMetricsCollectorState() *metricsCollectorState {
	return &metricsCollectorState{
		lastGPUSecondsUpdate:         time.Now(),
		nodes:                        nil,
		nodeUnavailabilityStartTimes: make(map[string]time.Time),
		nodeDrainingStartTimes:       make(map[string]time.Time),
	}
}
