package exporter

func buildExporterServiceAccountName(clusterName string) string {
	return clusterName + "-exporter-sa"
}

func buildExporterRoleName(clusterName string) string {
	return clusterName + "-exporter-role"
}

func buildExporterRoleBindingName(clusterName string) string {
	return clusterName + "-exporter-role-binding"
}
