package consts

const (
	// EnvDockerEnabled tells node entrypoint scripts whether Docker components
	// should be available on the node.
	EnvDockerEnabled = "SOPERATOR_DOCKER_ENABLED"

	// DockerImageStorageMountPath is the fixed jail and daemon mount for OCI data.
	DockerImageStorageMountPath = "/mnt/image-storage"
)
