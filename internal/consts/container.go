package consts

const (
	ContainerNameSlurmctld         = SlurmctldName
	ContainerNameMunge             = Munge
	ContainerNameSlurmd            = SlurmdName
	ContainerNameSshd              = SshdName
	ContainerNameToolkitValidation = "toolkit-validation"
	ContainerNameNCCLBenchmark     = ncclBenchmark
	ContainerNamePopulateJail      = populateJail
	ContainerNameExporter          = Exporter

	ContainerSecurityContextCapabilitySysAdmin = "SYS_ADMIN"

	ContainerPortNameExporter = "metrics"
	ContainerPortExporter     = 8080
	ContainerPathExporter     = "/metrics"
	ContainerSchemeExporter   = "http"
)
