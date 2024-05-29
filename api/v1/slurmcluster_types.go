/*
Copyright 2024.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SlurmClusterSpec defines the desired state of SlurmCluster
type SlurmClusterSpec struct {
	// ControllerNode defines the desired state of SlurmCluster controller nodes
	// kubebuilder:validation:Required
	ControllerNode ControllerNodeSpec `json:"controllerNode"`
}

// ControllerNodeSpec defines the desired state of SlurmCluster controller nodes
type ControllerNodeSpec struct {
	// Size defines the number of controller node instances
	// TODO remove maximum when we're ready for it
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1
	// +kubebuilder:validation:ExclusiveMinimum=false
	// +kubebuilder:validation:ExclusiveMaximum=false
	Size int32 `json:"size,omitempty"`

	// Image defines the image used for controller node
	Image *ImageSpec `json:"image,omitempty"`

	// Pod defines the spec for controller pods
	Pod *PodSpec `json:"pod,omitempty"`
}

const (
	ConditionClusterControllersAvailable = "ControllersAvailable"
	ConditionClusterWorkersAvailable     = "WorkersAvailable"

	PhaseClusterReconciling  = "Reconciling"
	PhaseClusterNotAvailable = "Not available"
	PhaseClusterAvailable    = "Available"
)

// SlurmClusterStatus defines the observed state of SlurmCluster
type SlurmClusterStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	Phase *string `json:"phase,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// SlurmCluster is the Schema for the slurmclusters API
type SlurmCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SlurmClusterSpec   `json:"spec,omitempty"`
	Status SlurmClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SlurmClusterList contains a list of SlurmCluster
type SlurmClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SlurmCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SlurmCluster{}, &SlurmClusterList{})
}
