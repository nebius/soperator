package v1alpha1

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// Image defines the container image and its pull policy
type Image struct {
	// Repository defines the full name (with repository) of container image.
	// Only the image name could be provided if needed.
	//
	// +kubebuilder:validation:Required
	Repository string `json:"repository"`

	// Tag defines the image tag
	//
	// +kubebuilder:validation:Optional
	Tag string `json:"tag,omitempty"`

	// PullPolicy defines the image pull policy
	//
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="IfNotPresent"
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
}

// StatusMetadata holds metadata of application update status
// +k8s:openapi-gen=true
type StatusMetadata struct {
	// UpdateStatus defines a status for update rollout
	UpdateStatus UpdateStatus `json:"updateStatus,omitempty"`
	// Reason defines human readable error reason
	Reason string `json:"reason,omitempty"`
	// ObservedGeneration defines current generation picked by operator for the reconcile
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Jobs maps child job identifier to its status
	Jobs map[string]batchv1.JobStatus `json:"jobs,omitempty"`
}

// UpdateStatus defines status for application
type UpdateStatus string

const (
	UpdateStatusUpdating  UpdateStatus = "updating"
	UpdateStatusFailed    UpdateStatus = "failed"
	UpdateStatusSucceeded UpdateStatus = "succeeded"
)

// ContainerSecuritySpec defines the security configuration for a container
type ContainerSecuritySpec struct {
	// SecurityLimitsConfig defines the multiline content of "limits.conf".
	// A line should have the following format:
	//	* <soft|hard> <item> <value>
	//
	// Example:
	//	* soft nofile 1024
	//
	// +kubebuilder:validation:Optional
	LimitsConfig string `json:"limitsConfig,omitempty"`

	// AppArmorProfile defines the AppArmor profile for the container
	//
	// +kubebuilder:default="unconfined"
	// +kubebuilder:validation:Optional
	AppArmorProfile string `json:"appArmorProfile,omitempty"`
}
