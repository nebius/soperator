package consts

const (
	ContainerNameSlurmctld         = SlurmctldName
	ContainerNameAccounting        = AccountingName
	ContainerNameMunge             = Munge
	ContainerNameSlurmd            = SlurmdName
	ContainerNameREST              = Slurmrestd
	ContainerNameSshd              = SshdName
	ContainerNameToolkitValidation = "toolkit-validation"
	ContainerNameWaitForController = "wait-for-controller"
	ContainerNameNCCLBenchmark     = ncclBenchmark
	ContainerNamePopulateJail      = populateJail
	ContainerNameExporter          = Exporter
	ContainerNameNodeSysctl        = "node-sysctl"
	ContainerNameRebooter          = "rebooter"
	ContainerNameNodeSysctlSleep   = "node-sysctl-sleep"
	ContainerNameSConfigController = SConfigControllerName

	ContainerSecurityContextCapabilitySysAdmin = "SYS_ADMIN"

	ContainerPortNameExporter = "metrics"
	ContainerPortExporter     = 8080
	ContainerPathExporter     = "/metrics"
	ContainerSchemeExporter   = "http"
)
