package exporter

import (
	"time"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

// metricsCollectorState holds the raw data collected from SLURM APIs
type metricsCollectorState struct {
	lastGPUSecondsUpdate         time.Time
	nodes                        []slurmapi.Node
	nodesCollectionSequence      uint64
	jobs                         []slurmapi.Job
	jobsCollectionSequence       uint64
	diag                         *slurmapi.Diag
	diagCollectionSequence       uint64
	nodeTopologies               map[string]NodeTopology
	topologyCollectionSequence   uint64
	nodeUnavailabilityStartTimes map[string]time.Time
	nodeDrainingStartTimes       map[string]time.Time
}

// newMetricsCollectorState initializes a new metrics collector state
func newMetricsCollectorState() *metricsCollectorState {
	return &metricsCollectorState{
		lastGPUSecondsUpdate:         time.Now(),
		nodes:                        nil,
		nodeTopologies:               make(map[string]NodeTopology),
		nodeUnavailabilityStartTimes: make(map[string]time.Time),
		nodeDrainingStartTimes:       make(map[string]time.Time),
	}
}
