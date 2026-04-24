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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RollingUpdateStateSpec defines the desired state of RollingUpdateState.
type RollingUpdateStateSpec struct {
	StatefulSetRef string `json:"statefulSetRef"`

	RemainingPods []string `json:"remainingPods"`
}

// RollingUpdateStateStatus defines the observed state of RollingUpdateState.
type RollingUpdateStateStatus struct {
	RemainingPodsCount int `json:"remainingPodsCount"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// RollingUpdateState is the Schema for the rollingupdatestates API.
type RollingUpdateState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RollingUpdateStateSpec   `json:"spec,omitempty"`
	Status RollingUpdateStateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RollingUpdateStateList contains a list of RollingUpdateState.
type RollingUpdateStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RollingUpdateState `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RollingUpdateState{}, &RollingUpdateStateList{})
}
