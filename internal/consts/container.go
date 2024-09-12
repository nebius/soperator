package consts

const (
	ContainerNameSlurmctld         = SlurmctldName
	ContainerNameAccounting        = AccountingName
	ContainerNameMunge             = Munge
	ContainerNameSlurmd            = SlurmdName
	ContainerNameSshd              = SshdName
	ContainerNameToolkitValidation = "toolkit-validation"
	ContainerNameNCCLBenchmark     = ncclBenchmark
	ContainerNamePopulateJail      = populateJail
	ContainerNameExporter          = Exporter
	ContainerNameCgroupMaker       = CgroupMaker

	ContainerSecurityContextCapabilitySysAdmin = "SYS_ADMIN"

	ContainerPortNameExporter = "metrics"
	ContainerPortExporter     = 8080
	ContainerPathExporter     = "/metrics"
	ContainerSchemeExporter   = "http"
)
