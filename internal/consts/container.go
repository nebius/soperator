package consts

const (
	ContainerNameSlurmctld         = SlurmctldName
	ContainerNameAccounting        = AccountingName
	ContainerNameMunge             = Munge
	ContainerNameSSSD              = "sssd"
	ContainerNameSlurmd            = SlurmdName
	ContainerNameREST              = Slurmrestd
	ContainerNameSshd              = SshdName
	ContainerNameWorkerInit        = "worker-init"
	ContainerNameDockerProxy       = "docker-proxy"
	ContainerNameWaitForDatabase   = "wait-for-database"
	ContainerNameWaitForAccounting = "wait-for-accounting"
	ContainerNamePopulateJail      = populateJail
	ContainerNameExporter          = Exporter
	ContainerNameRebooter          = "rebooter"
	ContainerNameCustom            = "custom-container"
	ContainerNameSConfigController = SConfigControllerName

	ContainerSecurityContextCapabilitySysAdmin = "SYS_ADMIN"

	ContainerPortNameExporter = "metrics"
	ContainerPortExporter     = 8080
	ContainerPathExporter     = "/metrics"

	ContainerPortNameMonitoring = "monitoring"
	ContainerPortMonitoring     = 8081
	ContainerPathMonitoring     = "/metrics"
)
