package consts

const (
	AnnotationVersions = "versions"

	AnnotationApparmorKey          = "container.apparmor.security.beta.kubernetes.io"
	DefaultContainerAnnotationName = "kubectl.kubernetes.io/default-container"
	AnnotationClusterName          = "slurm.nebius.ai/cluster"
	AnnotationActiveCheckKey       = "slurm.nebius.ai/activecheck"

	AnnotationSConfigControllerSourceKey = LabelSConfigControllerSourceKey + "/path"
	DefaultSConfigControllerSourcePath   = "/slurm"

	AnnotationSConfigControllerExecutableKey = LabelSConfigControllerSourceKey + "/executable"
	DefaultSConfigControllerExecutableValue  = "true"
)
