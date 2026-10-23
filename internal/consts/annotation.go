package consts

const (
	AnnotationVersions = "versions"

	AnnotationDefaultContainerName = "kubectl.kubernetes.io/default-container"
	AnnotationClusterName          = K8sGroupNameSoperator + "/cluster"
	AnnotationActiveCheckName      = K8sGroupNameSoperator + "/activecheck"

	AnnotationParentalClusterRefName = K8sGroupNameSoperator + "/parental-cluster-ref"

	// AnnotationManagedLabels and AnnotationManagedPodAnnotations record, on the workload object, which keys of its
	// labels and of its pod template annotations were rendered by the operator, so that the ones that are no longer
	// rendered can be removed without touching keys set by others.
	AnnotationManagedLabels         = K8sGroupNameSoperator + "/managed-labels"
	AnnotationManagedPodAnnotations = K8sGroupNameSoperator + "/managed-pod-annotations"
)
