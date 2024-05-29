package v1

import (
	corev1 "k8s.io/api/core/v1"
)

// ImageSpec defines the desired state of the image used for container
type ImageSpec struct {
	// Repository defines the path to the image
	Repository *string `json:"repository"`

	// Tag defines the tag of the image
	// +kubebuilder:default=latest
	Tag *string `json:"tag"`
}

// PodSpec defines the desired state of the common component pod specs
type PodSpec struct {
	// Cores defines the number of CPU cores
	Cores *string `json:"cores,omitempty"`

	// Memory defines the amount of RAM
	Memory *string `json:"memory,omitempty"`

	// PVCSize defines the disk size
	PVCSize *string `json:"pvcSize,omitempty"`

	// Affinity defines the desired affinity
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Affinity defines the desired tolerations
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}
