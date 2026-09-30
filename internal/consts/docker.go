package consts

const (
	// EnvDockerEnabled tells node entrypoint scripts whether Docker components should be available.
	EnvDockerEnabled = "SOPERATOR_DOCKER_ENABLED"

	// ImageStorageMountPath is the volume root shared by container runtimes.
	ImageStorageMountPath = "/mnt/image-storage"
)
