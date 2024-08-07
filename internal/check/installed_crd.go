package check

import "os"

var IsOpenTelemetryCollectorCRDInstalled = false

func IsOtelCRDInstalled() bool {
	IsOpenTelemetryCollectorCRDInstalled = os.Getenv("IS_OPENTELEMETRY_COLLECTOR_CRD_INSTALLED") == "true"
	return IsOpenTelemetryCollectorCRDInstalled
}
