/*
Copyright 2025 Nebius B.V.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TODO add docs
// +kubebuilder:validation:Enum=Reconfigure
type UpdateAction string

const (
	// TODO docs
	Reconfigure UpdateAction = "Reconfigure"
)

// TODO rework ObjectReference to bespoke reference type
// https://github.com/kubernetes/api/blob/release-1.17/admissionregistration/v1/types.go#L533
// TODO or to AggregateRule, and writing to self spec

// JailedConfigSpec defines the desired state of JailedConfig
// It is mostly same as corev1.ConfigMapVolumeSource, except it _requires_ absolute paths in `Items`,
// because there's no `VolumeMount` analog here, only `Volume`
type JailedConfigSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// TODO support multiple configmaps as a single jailed config, to force atomic reconfigures
	// TODO do we really-really need it?
	// TODO changes in a single ConfigMap could be applied non-atomically anyway
	// TODO and during initial cluster startup we can wait for multiple JailedConfigs to reach proper state
	// TODO fix docs
	// TODO use field reference instead of items?
	ConfigMap *corev1.ObjectReference `json:"configMap,omitempty"`

	// TODO fix docs
	// items if unspecified, each key-value pair in the Data field of the referenced
	// ConfigMap will be projected into the volume as a file whose name is the
	// key and content is the value. If specified, the listed keys will be
	// projected into the specified paths, and unlisted keys will not be
	// present. If a key is specified which is not present in the ConfigMap,
	// the volume setup will error unless it is marked optional. Paths must be
	// relative and may not contain the '..' path or start with '..'.
	// +optional
	// +listType=atomic
	Items []corev1.KeyToPath `json:"items,omitempty"`

	// TODO fix docs
	// defaultMode is optional: mode bits used to set permissions on created files by default.
	// Must be an octal value between 0000 and 0777 or a decimal value between 0 and 511.
	// YAML accepts both octal and decimal values, JSON requires decimal values for mode bits.
	// Defaults to 0644.
	// Directories within the path are not affected by this setting.
	// This might be in conflict with other options that affect the file
	// mode, like fsGroup, and the result can be other mode bits set.
	// +optional
	DefaultMode *int32 `json:"defaultMode,omitempty"`

	// +optional
	// +listType=atomic
	UpdateActions []UpdateAction `json:"updateActions,omitempty"`
}

type JailedConfigConditionType string

const (
	// FilesWritten indicates whether all files from this config were written to jail
	FilesWritten JailedConfigConditionType = "FilesWritten"
)

// JailedConfigStatus defines the observed state of JailedConfig.
type JailedConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Current state of jailed config
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// JailedConfig is the Schema for the jailedconfigs API
type JailedConfig struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of JailedConfig
	// +required
	Spec JailedConfigSpec `json:"spec"`

	// status defines the observed state of JailedConfig
	// +optional
	Status JailedConfigStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// JailedConfigList contains a list of JailedConfig
type JailedConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []JailedConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&JailedConfig{}, &JailedConfigList{})
}
