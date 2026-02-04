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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	KindNodeConfigurator = "NodeConfigurator"
)

// NodeConfiguratorSpec defines the desired state of NodeConfigurator.
type NodeConfiguratorSpec struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=true
	HostNetwork bool `json:"hostNetwork"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	HostIPC bool `json:"hostIPC"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	ShareProcessNamespace bool `json:"shareProcessNamespace"`

	// Rebooter controller which will reboot and drain node by some node conditions
	// in same time can be used rebooter or nodeConfigurator
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:={enabled: true}
	Rebooter Rebooter `json:"rebooter"`

	// CustomContainer defines container for the node
	// in same time can be used rebooter or customContainer
	//
	// +kubebuilder:validation:Optional
	CustomContainer CustomContainer `json:"customContainer"`

	// InitContainers defines the list of initContainers for the node-configurator
	// it rewrite the default initContainers
	// All applied init containers will be lexicographically ordered by their names
	//
	// +kubebuilder:validation:Optional
	InitContainers []corev1.Container `json:"initContainers,omitempty"`
}

type ContainerConfig struct {
	// Name defines the name of container
	//
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`

	// Image defines the container image
	//
	// +kubebuilder:validation:Optional
	Image Image `json:"image,omitempty"`

	// Command defines the command of container
	// +kubebuilder:validation:Optional
	Command []string `json:"command,omitempty"`

	// Resources defines the [corev1.ResourceRequirements] for the container
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:={}
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// LivenessProbe defines the livenessProbe for the node-configurator
	//
	// +kubebuilder:validation:Optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// ReadinessProbe defines the readinessProbe for the node-configurator
	//
	// +kubebuilder:validation:Optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// Env defines the list of environment variables for the node-configurator
	//
	// +kubebuilder:validation:Optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Tolerations defines the list of tolerations for the node-configurator
	//
	// +kubebuilder:validation:Optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// NodeSelector defines the nodeSelector for the node-configurator
	//
	// +kubebuilder:validation:Optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Affinity defines the affinity for the node-configurator
	//
	// +kubebuilder:validation:Optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

type PodConfig struct {
	// PriorityClassName defines the priorityClassName for the pod
	//
	// +kubebuilder:validation:Optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// ServiceAccountName defines the service account name for the pod
	//
	// +kubebuilder:validation:Optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// HostUsers controls if the pod containers can use the host user namespace
	//
	// +kubebuilder:validation:Optional
	HostUsers *bool `json:"hostUsers,omitempty"`
}

type Rebooter struct {
	Enabled         bool `json:"enabled"`
	ContainerConfig `json:",inline"`
	PodConfig       `json:",inline"`
	// EvictionMethod defines the method of eviction for the Slurm worker node
	// Must be one of [drain, evict]. Now only evict is supported
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum="evict"
	// +kubebuilder:default="evict"
	EvictionMethod string `json:"evictionMethod,omitempty"`

	// LogLevel defines the log level for the node-configurator
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="info"
	// +kubebuilder:validation:Enum="debug";"info";"warn";"error"
	LogLevel string `json:"logLevel,omitempty"`

	// LogFormat defines the log format for the node-configurator
	//
	// +kubebuilder:validation:Optional
	LogFormat string `json:"logFormat,omitempty"`
}

type CustomContainer struct {
	Enabled         bool `json:"enabled"`
	ContainerConfig `json:",inline"`
	PodConfig       `json:",inline"`
}

// NodeConfigurator is the Schema for the nodeconfigurators API.
// +kubebuilder:printcolumn:name="Rebooter Enabled",type="boolean",JSONPath=".spec.rebooter.enabled",description="Whether rebooter is enabled"
// +kubebuilder:printcolumn:name="NodeConfigurator Enabled",type="boolean",JSONPath=".spec.nodeConfigurator.enabled",description="Whether nodeConfigurator is enabled"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type NodeConfigurator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeConfiguratorSpec   `json:"spec,omitempty"`
	Status NodeConfiguratorStatus `json:"status,omitempty"`
}

// NodeConfiguratorStatus defines the observed state of NodeConfigurator.
type NodeConfiguratorStatus struct {
	StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *NodeConfiguratorStatus) GetStatusMetadata() *StatusMetadata {
	return &cr.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GetObjectKind implements runtime.Object.
// Subtle: this method shadows the method (TypeMeta).GetObjectKind of NodeConfigurator.TypeMeta.
func (n *NodeConfigurator) GetObjectKind() schema.ObjectKind {
	return &n.TypeMeta
}

func (n *NodeConfigurator) DeepCopyObject() runtime.Object {
	if c := n.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// +kubebuilder:object:root=true

// NodeConfiguratorList contains a list of NodeConfigurator.
type NodeConfiguratorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeConfigurator `json:"items"`
}

// GetObjectKind implements runtime.Object.
// Subtle: this method shadows the method (TypeMeta).GetObjectKind of NodeConfiguratorList.TypeMeta.
func (n *NodeConfiguratorList) GetObjectKind() schema.ObjectKind {
	return &n.TypeMeta
}

func init() {
	SchemeBuilder.Register(&NodeConfigurator{}, &NodeConfiguratorList{})
}
