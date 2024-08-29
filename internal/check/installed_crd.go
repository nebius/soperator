package check

import (
	"os"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

var IsOpenTelemetryCollectorCRDInstalled = false
var IsPrometheusOperatorCRDInstalled = false

func IsOtelCRDInstalled() bool {
	IsOpenTelemetryCollectorCRDInstalled = os.Getenv("IS_OPENTELEMETRY_COLLECTOR_CRD_INSTALLED") == "true"
	return IsOpenTelemetryCollectorCRDInstalled
}

func IsPrometheusCRDInstalled() bool {
	IsPrometheusOperatorCRDInstalled = os.Getenv("IS_PROMETHEUS_CRD_INSTALLED") == "true"
	return IsPrometheusOperatorCRDInstalled
}

func IsPrometheusEnabled(telemetry *slurmv1.Telemetry) bool {
	if telemetry != nil && telemetry.Prometheus != nil && telemetry.Prometheus.Enabled {
		return true
	}
	return false
}

func IsOtelEnabled(telemetry *slurmv1.Telemetry) bool {
	if telemetry != nil && telemetry.OpenTelemetryCollector != nil && telemetry.OpenTelemetryCollector.Enabled {
		return true
	}
	return false
}
