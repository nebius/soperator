package exporter

func BuildExporterServiceAccountName(clusterName string) string {
	return clusterName + "-exporter-sa"
}

func BuildExporterRoleName(clusterName string) string {
	return clusterName + "-exporter-role"
}

func BuildExporterRoleBindingName(clusterName string) string {
	return clusterName + "-exporter-role-binding"
}
