package v1

// ImageSpec defines the desired state of the image used for container
type ImageSpec struct {
	// Repository defines the path to the image
	// +kubebuilder:default=cr.nemax.nebius.cloud/crnonjecps8pifr7am4i/controller
	Repository string `json:"repository"`

	// Tag defines the tag of the image
	// +kubebuilder:default=latest
	Tag string `json:"tag"`
}
