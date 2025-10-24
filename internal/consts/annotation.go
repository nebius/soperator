package consts

const (
	AnnotationVersions = "versions"

	AnnotationApparmorKey          = "container.apparmor.security.beta.kubernetes.io"
	AnnotationDefaultContainerName = "kubectl.kubernetes.io/default-container"
	AnnotationClusterName          = K8sGroupNameSoperator + "/cluster"
	AnnotationActiveCheckName      = K8sGroupNameSoperator + "/activecheck"
)
