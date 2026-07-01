package consts

const (
	AnnotationVersions = "versions"

	AnnotationDefaultContainerName = "kubectl.kubernetes.io/default-container"
	AnnotationClusterName          = K8sGroupNameSoperator + "/cluster"
	AnnotationActiveCheckName      = K8sGroupNameSoperator + "/activecheck"
	AnnotationDependencyVersion    = K8sGroupNameSoperator + "/dependency-version"

	AnnotationDependencyVersionName = "name"

	AnnotationParentalClusterRefName = K8sGroupNameSoperator + "/parental-cluster-ref"

	AnnotationSoperatorRollingUpdateMaxUnavailable = K8sGroupNameSoperator + "/rolling-update-max-unavailable"
)
