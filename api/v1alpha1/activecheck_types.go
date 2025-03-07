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
	// Schedule defines the CronJob schedule.
	// By default, every year - at 00:00 on day-of-month 1 in January
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="0 0 1 1 *"
	Schedule string `json:"schedule,omitempty"`

	// Suspend indicates whether the action is suspended.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=true
	Suspend bool `json:"suspend,omitempty"`

	// RunAfterCreation specifies whether the job should run immediately after the CronJob is created.
	// +kubebuilder:default=true
	RunAfterCreation bool `json:"runAfterCreation,omitempty"`

	// PodTemplateNameRef points to a PodTemplate that contains the pod configuration. Use it to override the default settings.
	// +kubebuilder:validation:Optional
	PodTemplateNameRef *string `json:"podTemplateNameRef,omitempty"`

	// SpecNCCLTests defines settings for specific type of the check
	// +kubebuilder:validation:Optional
	SpecNCCLTests SpecNCCLTests `json:"specNCCLTests,omitempty"`

	// Reactions defines reaction on specific check
	// +kubebuilder:validation:Optional
	Reactions Reactions `json:"reactions,omitempty"`
}

type Reactions struct {
	// SetCondition enabling setting condition to the k8s node
	SetCondition bool `json:"setCondition,omitempty"`

	// DrainSlurmNode enabling slurm node draining if check failed
	DrainSlurmNode bool `json:"DrainSlurmNode,omitempty"`
}

type SpecNCCLTests struct {
	// Name + CheckType defines the name of k8s cronJob
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=nccl-test
	Name string `json:"name,omitempty"`

	// Priority defines the priority of k8s cronJob
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=10
	Priority int32 `json:"priority,omitempty"`

	// CheckType defines the name of the binary that will be executed during tests.
	// +kubebuilder:validation:Enum=all_gather_perf;all_reduce_perf;alltoall_perf;broadcast_perf;gather_perf;hypercube_perf;reduce_perf;reduce_scatter_perf;scatter_perf;sendrecv_perf;all_gather_perf_mpi;all_reduce_perf_mpi;alltoall_perf_mpi;broadcast_perf_mpi;gather_perf_mpi;hypercube_perf_mpi;reduce_perf_mpi;reduce_scatter_perf_mpi;scatter_perf_mpi;sendrecv_perf_mpi
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=all_reduce_perf
	CheckType string `json:"checkType,omitempty"`

	// Args defines raw params string
	// +kubebuilder:validation:Optional
	Args string `json:"args,omitempty"`
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
