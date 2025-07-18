package check

import (
	"os"

	"nebius.ai/slurm-operator/internal/values"
)

var (
	IsPrometheusOperatorCRDInstalled = false
	IsMariaDbOperatorCRDInstalled    = false
	IsAppArmorOperatorCRDInstalled   = false
)

func IsPrometheusCRDInstalled() bool {
	IsPrometheusOperatorCRDInstalled = os.Getenv("IS_PROMETHEUS_CRD_INSTALLED") == "true"
	return IsPrometheusOperatorCRDInstalled
}

func IsMariaDbCRDInstalled() bool {
	IsMariaDbOperatorCRDInstalled = os.Getenv("IS_MARIADB_CRD_INSTALLED") == "true"
	return IsMariaDbOperatorCRDInstalled
}

func IsAppArmorCRDInstalled() bool {
	IsAppArmorOperatorCRDInstalled = os.Getenv("IS_APPARMOR_CRD_INSTALLED") == "true"
	return IsAppArmorOperatorCRDInstalled
}

func IsPrometheusEnabled(exporter *values.SlurmExporter) bool {
	if exporter != nil && exporter.Enabled {

		return true
	}
	return false
}
