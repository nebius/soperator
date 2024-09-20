package check

import (
	"os"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/values"
)

var (
	IsOpenTelemetryCollectorCRDInstalled = false
	IsPrometheusOperatorCRDInstalled     = false
	IsMariaDbOperatorCRDInstalled        = false
)

func IsOtelCRDInstalled() bool {
	IsOpenTelemetryCollectorCRDInstalled = os.Getenv("IS_OPENTELEMETRY_COLLECTOR_CRD_INSTALLED") == "true"
	return IsOpenTelemetryCollectorCRDInstalled
}

func IsPrometheusCRDInstalled() bool {
	IsPrometheusOperatorCRDInstalled = os.Getenv("IS_PROMETHEUS_CRD_INSTALLED") == "true"
	return IsPrometheusOperatorCRDInstalled
}

func IsMariaDbCRDInstalled() bool {
	IsMariaDbOperatorCRDInstalled = os.Getenv("IS_MARIADB_CRD_INSTALLED") == "true"
	return IsMariaDbOperatorCRDInstalled
}

func IsPrometheusEnabled(exporter *values.SlurmExporter) bool {
	if exporter != nil && exporter.Enabled {

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
