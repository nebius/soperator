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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	KindResourcePatchPolicy = "ResourcePatchPolicy"

	// ConditionTypeResourcePatchPolicyAccepted indicates whether the policy
	// passed static validation and was accepted by the operator.
	ConditionTypeResourcePatchPolicyAccepted = "Accepted"
	// ConditionTypeResourcePatchPolicyApplied indicates whether all patch
	// entries were applied successfully during the last reconciliation of the
	// target resource.
	ConditionTypeResourcePatchPolicyApplied = "Applied"
)

// PatchType is the patch mechanism used by a ResourcePatchPolicy.
// +kubebuilder:validation:Enum=JSONPatch;JSONMergePatch
type PatchType string

const (
	// JSONPatchType applies RFC 6902 JSON Patch operations.
	JSONPatchType PatchType = "JSONPatch"
	// JSONMergePatchType applies an RFC 7386 JSON Merge Patch object.
	JSONMergePatchType PatchType = "JSONMergePatch"
)

// ResourcePatchPolicySpec defines the desired state of ResourcePatchPolicy.
type ResourcePatchPolicySpec struct {
	// TargetRef identifies the SlurmCluster, NodeSet or NodeConfigurator this
	// policy applies to.
	TargetRef PolicyTargetReference `json:"targetRef"`

	// Priority determines the order in which policies are applied to the same
	// resource. Lower values are applied first.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=0
	Priority *int32 `json:"priority,omitempty"`

	// Type is the patch mechanism: "JSONPatch" (RFC 6902) or "JSONMergePatch"
	// (RFC 7386).
	Type PatchType `json:"type"`

	// Patches is the list of patches to apply to generated resources.
	//
	// +kubebuilder:validation:MinItems=1
	Patches []ResourcePatch `json:"patches"`
}

// PolicyTargetReference points at the operator-managed parent object a policy
// attaches to.
type PolicyTargetReference struct {
	// Group is the API group of the target resource.
	//
	// +kubebuilder:default="slurm.nebius.ai"
	Group string `json:"group"`

	// Kind is the kind of the target resource.
	//
	// +kubebuilder:validation:Enum=SlurmCluster;NodeSet;NodeConfigurator
	Kind string `json:"kind"`

	// Name is the name of the target resource.
	//
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace is the namespace of the target resource. Defaults to the
	// namespace of the ResourcePatchPolicy.
	//
	// +kubebuilder:validation:Optional
	Namespace *string `json:"namespace,omitempty"`
}

// ResourcePatch targets a specific generated Kubernetes resource.
type ResourcePatch struct {
	// ResourceRef selects which generated Kubernetes resource to patch.
	ResourceRef ResourceSelector `json:"resourceRef"`

	// JSONPatch contains RFC 6902 operations. Used when type is "JSONPatch".
	//
	// +kubebuilder:validation:Optional
	JSONPatch []JSONPatchOperation `json:"jsonPatch,omitempty"`

	// JSONMergePatch contains an RFC 7386 merge patch object. Used when type is
	// "JSONMergePatch".
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:pruning:PreserveUnknownFields
	JSONMergePatch *apiextensionsv1.JSON `json:"jsonMergePatch,omitempty"`
}

// ResourceSelector selects generated Kubernetes resources by kind and name.
type ResourceSelector struct {
	// Kind of the generated Kubernetes resource (e.g. StatefulSet, Service,
	// ConfigMap, DaemonSet).
	//
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// Name is the exact name of the generated resource to patch. When empty,
	// all resources of the given kind are matched.
	//
	// +kubebuilder:validation:Optional
	Name *string `json:"name,omitempty"`

	// APIVersion of the resource (e.g. "apps/v1"). When set, it must match the
	// generated resource's apiVersion. Defaults to matching any version.
	//
	// +kubebuilder:validation:Optional
	APIVersion *string `json:"apiVersion,omitempty"`
}

// JSONPatchOperation is a single RFC 6902 operation.
type JSONPatchOperation struct {
	// Op is the operation: "add", "remove", "replace", "move", "copy", "test".
	//
	// +kubebuilder:validation:Enum=add;remove;replace;move;copy;test
	Op string `json:"op"`

	// Path is the JSON Pointer (RFC 6901) to the target field.
	//
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`

	// Value is the value to apply. Required for add, replace and test.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Value *apiextensionsv1.JSON `json:"value,omitempty"`

	// From is the source path for move and copy operations.
	//
	// +kubebuilder:validation:Optional
	From *string `json:"from,omitempty"`
}

// ResourcePatchPolicyStatus defines the observed state of ResourcePatchPolicy.
type ResourcePatchPolicyStatus struct {
	// Conditions describe the current state of the policy.
	//
	// +kubebuilder:validation:Optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// PatchedResources lists the resources matched during the last
	// reconciliation of the target and whether each patch was applied.
	//
	// +kubebuilder:validation:Optional
	PatchedResources []PatchedResourceStatus `json:"patchedResources,omitempty"`
}

// PatchedResourceStatus reports the outcome of patching a single resource.
type PatchedResourceStatus struct {
	// Resource identifies the patched resource.
	Resource corev1.ObjectReference `json:"resource"`

	// Applied indicates whether the patch was applied successfully.
	Applied bool `json:"applied"`

	// Message provides details, typically on failure.
	//
	// +kubebuilder:validation:Optional
	Message *string `json:"message,omitempty"`
}

// ResourcePatchPolicy enables modifications to Kubernetes resources generated
// by soperator for SlurmCluster, NodeSet and NodeConfigurator objects.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target Kind",type="string",JSONPath=".spec.targetRef.kind",description="Kind of the target resource"
// +kubebuilder:printcolumn:name="Target Name",type="string",JSONPath=".spec.targetRef.name",description="Name of the target resource"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type",description="Patch type"
// +kubebuilder:printcolumn:name="Accepted",type="string",JSONPath=".status.conditions[?(@.type==\"Accepted\")].status",description="Whether the policy was accepted"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ResourcePatchPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourcePatchPolicySpec   `json:"spec"`
	Status ResourcePatchPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourcePatchPolicyList contains a list of ResourcePatchPolicy.
type ResourcePatchPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourcePatchPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourcePatchPolicy{}, &ResourcePatchPolicyList{})
}
