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

// UpdateAction is a single action that can be performed after materializing files
// +kubebuilder:validation:Enum=Reconfigure
type UpdateAction string

const (
	// UpdateActionReconfigure will call reconfigure endpoint and wait until slurmd restarts on every node
	// See https://slurm.schedmd.com/rest_api.html#slurmV0043GetReconfigure
	// See https://slurm.schedmd.com/rest_api.html#slurmV0043GetNodes
	UpdateActionReconfigure UpdateAction = "Reconfigure"
)

// ConfigMapReference holds a reference to v1.ConfigMap
// There's no Namespace field because JailedConfig and ConfigMap must be in same namespace
type ConfigMapReference struct {
	// `name` is the name of the ConfigMap.
	// +required
	Name string `json:"name"`
}

// JailedConfigSpec defines the desired state of JailedConfig
// It is mostly same as corev1.ConfigMapVolumeSource, except it _requires_ absolute paths in `Items`,
// because there's no `VolumeMount` analog here, only `Volume`
type JailedConfigSpec struct {
	// Reference to ConfigMap to read content from
	// This does not use corev1.ObjectReference, because ObjectReference itself so:
	// > Instead of using this type, create a locally provided and used type that is well-focused on your reference.
	// +required
	ConfigMap ConfigMapReference `json:"configMap,omitempty"`

	// If unspecified, each key-value pair in the Data field of the referenced
	// ConfigMap will be materialized as a file whose name is the
	// key and content is the value. If specified, the listed keys will be
	// materialized into the specified paths, and unlisted keys will not be
	// present. If a key is specified which is not present in the ConfigMap,
	// the jailed config setup will error. Paths must be
	// absolute and may not contain the '.' or '..' segments
	// +optional
	// +listType=atomic
	Items []corev1.KeyToPath `json:"items,omitempty"`

	// defaultMode is optional: mode bits used to set permissions on created files by default.
	// Must be an octal value between 0000 and 0777 or a decimal value between 0 and 511.
	// YAML accepts both octal and decimal values, JSON requires decimal values for mode bits.
	// Defaults to 0644.
	// +optional
	DefaultMode *int32 `json:"defaultMode,omitempty"`

	// updateActions are optional: it is a list of action to perform after materializing files
	// They will be performed sequentially, in same order as in spec
	// `Reconfigure` will call reconfigure endpoint and wait until slurmd restarts on every node
	// See https://slurm.schedmd.com/rest_api.html#slurmV0043GetReconfigure
	// See https://slurm.schedmd.com/rest_api.html#slurmV0043GetNodes
	// +optional
	// +listType=atomic
	UpdateActions []UpdateAction `json:"updateActions,omitempty"`
}

// JailedConfigStatus defines the observed state of JailedConfig.
type JailedConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
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
