package exporter

import (
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

// metricsCollectorState holds the raw data collected from SLURM APIs
type metricsCollectorState struct {
	lastGPUSecondsUpdate         time.Time
	nodes                        []slurmapi.Node
	jobs                         []slurmapi.Job
	diag                         *api.V0041OpenapiDiagResp
	nodeUnavailabilityStartTimes map[string]time.Time
	nodeDrainingStartTimes       map[string]time.Time
	nodeUnavailabilityMetrics    map[string]float64
	nodeDrainingMetrics          map[string]float64
}

// newMetricsCollectorState initializes a new metrics collector state
func newMetricsCollectorState() *metricsCollectorState {
	return &metricsCollectorState{
		lastGPUSecondsUpdate:         time.Now(),
		nodes:                        nil,
		nodeUnavailabilityStartTimes: make(map[string]time.Time),
		nodeDrainingStartTimes:       make(map[string]time.Time),
		nodeUnavailabilityMetrics:    make(map[string]float64),
		nodeDrainingMetrics:          make(map[string]float64),
	}
}
