package consts

const (
	ContainerNameSlurmctld         = SlurmctldName
	ContainerNameAccounting        = AccountingName
	ContainerNameMunge             = Munge
	ContainerNameSlurmd            = SlurmdName
	ContainerNameREST              = Slurmrestd
	ContainerNameSshd              = SshdName
	ContainerNameToolkitValidation = "toolkit-validation"
	ContainerNameNCCLBenchmark     = ncclBenchmark
	ContainerNamePopulateJail      = populateJail
	ContainerNameExporter          = Exporter
	ContainerNameNodeSysctl        = "node-sysctl"
	ContainerNameRebooter          = "rebooter"
	ContainerNameNodeSysctlSleep   = "node-sysctl-sleep"

	ContainerSecurityContextCapabilitySysAdmin = "SYS_ADMIN"

	ContainerPortNameExporter = "metrics"
	ContainerPortExporter     = 8080
	ContainerPathExporter     = "/metrics"
	ContainerSchemeExporter   = "http"
)
