package exporter

import (
	"time"
)

type metricsCollectorState struct {
	lastUpdateTime time.Time
}

// newMetricsCollectorState initializes a new metrics collector state
func newMetricsCollectorState() metricsCollectorState {
	return metricsCollectorState{
		lastUpdateTime: time.Now(),
	}
}
