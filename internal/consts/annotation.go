package consts

const (
	AnnotationVersions = "versions"

	AnnotationApparmorKey          = "container.apparmor.security.beta.kubernetes.io"
	DefaultContainerAnnotationName = "kubectl.kubernetes.io/default-container"
	AnnotationClusterName          = "slurm.nebius.ai/cluster"

	AnnotationReflectorAllowed           = "reflector.v1.k8s.emberstack.com/reflection-allowed"
	AnnotationReflectorAllowedNamespaces = "reflector.v1.k8s.emberstack.com/reflection-allowed-namespaces"
)
