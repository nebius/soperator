package consts

const (
	ContainerNameSlurmctld         = SlurmctldName
	ContainerNameAccounting        = AccountingName
	ContainerNameMunge             = Munge
	ContainerNameSlurmd            = SlurmdName
	ContainerNameREST              = Slurmrestd
	ContainerNameSshd              = SshdName
	ContainerNameWaitForController = "wait-for-controller"
	ContainerNamePopulateJail      = populateJail
	ContainerNameExporter          = Exporter
	ContainerNameRebooter          = "rebooter"
	ContainerNameCustom            = "custom-container"
	ContainerNameSConfigController = SConfigControllerName

	ContainerSecurityContextCapabilitySysAdmin = "SYS_ADMIN"

	ContainerPortNameExporter = "metrics"
	ContainerPortExporter     = 8080
	ContainerPathExporter     = "/metrics"
	ContainerSchemeExporter   = "http"

	ContainerPortNameMonitoring = "monitoring"
	ContainerPortMonitoring     = 8081
	ContainerPathMonitoring     = "/metrics"
	ContainerSchemeMonitoring   = "http"
)
