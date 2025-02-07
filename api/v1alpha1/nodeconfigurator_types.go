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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NodeConfiguratorSpec defines the desired state of NodeConfigurator.
type NodeConfiguratorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of NodeConfigurator. Edit nodeconfigurator_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

// NodeConfiguratorStatus defines the observed state of NodeConfigurator.
type NodeConfiguratorStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// NodeConfigurator is the Schema for the nodeconfigurators API.
type NodeConfigurator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeConfiguratorSpec   `json:"spec,omitempty"`
	Status NodeConfiguratorStatus `json:"status,omitempty"`
}

// DeepCopyObject implements runtime.Object.
func (n *NodeConfigurator) DeepCopyObject() runtime.Object {
	panic("unimplemented")
}

// GetObjectKind implements runtime.Object.
// Subtle: this method shadows the method (TypeMeta).GetObjectKind of NodeConfigurator.TypeMeta.
func (n *NodeConfigurator) GetObjectKind() schema.ObjectKind {
	panic("unimplemented")
}

// +kubebuilder:object:root=true

// NodeConfiguratorList contains a list of NodeConfigurator.
type NodeConfiguratorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeConfigurator `json:"items"`
}

// DeepCopyObject implements runtime.Object.
func (n *NodeConfiguratorList) DeepCopyObject() runtime.Object {
	panic("unimplemented")
}

// GetObjectKind implements runtime.Object.
// Subtle: this method shadows the method (TypeMeta).GetObjectKind of NodeConfiguratorList.TypeMeta.
func (n *NodeConfiguratorList) GetObjectKind() schema.ObjectKind {
	panic("unimplemented")
}

func init() {
	SchemeBuilder.Register(&NodeConfigurator{}, &NodeConfiguratorList{})
}
