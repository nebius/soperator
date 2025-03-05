/*
Copyright 2024 Nebius B.V.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ActiveCheckSpec defines the desired state of ActiveCheck.
type ActiveCheckSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of ActiveCheck. Edit activecheck_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

// ActiveCheckStatus defines the observed state of ActiveCheck.
type ActiveCheckStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ActiveCheck is the Schema for the activechecks API.
type ActiveCheck struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ActiveCheckSpec   `json:"spec,omitempty"`
	Status ActiveCheckStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ActiveCheckList contains a list of ActiveCheck.
type ActiveCheckList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ActiveCheck `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ActiveCheck{}, &ActiveCheckList{})
}
