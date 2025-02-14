package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Image defines container image and it's pull policy
type Image struct {
	// Repository contains name of container image + it's repository if needed
	Repository string `json:"repository,omitempty"`
	// Tag contains desired container image version
	Tag string `json:"tag,omitempty"`
	// PullPolicy describes how to pull container image
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
	// Known .status.conditions.type are: "Available", "Progressing", and "Degraded"
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []Condition `json:"conditions,omitempty"`
}

// Condition defines status condition of the resource
type Condition struct {
	// Type of condition in CamelCase or in name.namespace.resource.victoriametrics.com/CamelCase.
	// +required
	// +kubebuilder:validation:MaxLength=316
	Type string `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status"`
	// ObservedGeneration represents the .metadata.generation that the condition was set based upon.
	// For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9, the condition is out of date
	// with respect to the current state of the instance.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,3,opt,name=observedGeneration"`
	// LastTransitionTime is the last time the condition transitioned from one status to another.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// LastUpdateTime is the last time of given type update.
	// This value is used for status TTL update and removal
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Reason contains a programmatic identifier indicating the reason for the condition's last transition.
	// Producers of specific condition types may define expected values and meanings for this field,
	// and whether the values are considered a guaranteed API.
	// The value should be a CamelCase string.
	// This field may not be empty.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:MinLength=1
	Reason string `json:"reason"`
	// Message is a human readable message indicating details about the transition.
	// This may be an empty string.
	// +optional
	// +kubebuilder:validation:MaxLength=32768
	Message string `json:"message,omitempty"`
}

// UpdateStatus defines status for application
type UpdateStatus string

const (
	UpdateStatusUpdating  UpdateStatus = "updating"
	UpdateStatusFailed    UpdateStatus = "failed"
	UpdateStatusSucceeded UpdateStatus = "succeeded"
)
