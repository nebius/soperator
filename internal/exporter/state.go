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
	nodesCollectionSequence      uint64
	jobs                         []slurmapi.Job
	jobsCollectionSequence       uint64
	diag                         *api.V0041OpenapiDiagResp
	diagCollectionSequence       uint64
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
