package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="NodeSet",type="string",JSONPath=".spec.nodeSetRef",description="Reference to the NodeSet"
// +kubebuilder:printcolumn:name="Active",type="integer",JSONPath=".status.activeCount",description="Number of active nodes"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Ready status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NodeSetPowerState is the Schema for the nodesetpowerstates API.
// It manages the power state of nodes in a NodeSet for ephemeral node scaling.
// One NodeSetPowerState exists per NodeSet, providing a clear 1:1 mapping.
type NodeSetPowerState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeSetPowerStateSpec   `json:"spec,omitempty"`
	Status NodeSetPowerStateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NodeSetPowerStateList contains a list of NodeSetPowerState
type NodeSetPowerStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []NodeSetPowerState `json:"items"`
}

// NodeSetPowerStateSpec defines the desired state of NodeSetPowerState
type NodeSetPowerStateSpec struct {
	// NodeSetRef is the name of the NodeSet this power state applies to.
	// This creates a 1:1 mapping between NodeSetPowerState and NodeSet.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	NodeSetRef string `json:"nodeSetRef"`

	// ActiveNodes is the list of node ordinals that should be powered on.
	// The power-manager binary updates this list based on Slurm's ResumeProgram/SuspendProgram calls.
	// For example, [0, 3, 5, 7, 12] means nodes with ordinals 0, 3, 5, 7, and 12 should be active.
	//
	// +kubebuilder:validation:Optional
	// +listType=set
	ActiveNodes []int32 `json:"activeNodes,omitempty"`
}

// NodeSetPowerStateStatus defines the observed state of NodeSetPowerState
type NodeSetPowerStateStatus struct {
	// ObservedGeneration is the most recent generation observed for this NodeSetPowerState.
	// It corresponds to the NodeSetPowerState's generation, which is updated on mutation by the API Server.
	//
	// +kubebuilder:validation:Optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ActiveCount is the current number of nodes that are active (powered on).
	//
	// +kubebuilder:validation:Optional
	ActiveCount int32 `json:"activeCount,omitempty"`

	// NodeStates contains the detailed state of each active node, keyed by ordinal.
	//
	// +kubebuilder:validation:Optional
	NodeStates map[string]NodePowerStateInfo `json:"nodeStates,omitempty"`

	// Conditions represent the observations of a NodeSetPowerState's current state.
	// Known types are: Ready, ScalingInProgress.
	//
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchMergeKey:"type" patchStrategy:"merge"`
}

// NodePowerStateInfo contains the state information for a single node
type NodePowerStateInfo struct {
	// Phase indicates the current phase of the node's pod.
	// Maps to corev1.PodPhase: Pending, Running, Succeeded, Failed, Unknown.
	//
	// +kubebuilder:validation:Optional
	Phase NodePowerPhase `json:"phase,omitempty"`

	// PodName is the name of the pod associated with this node.
	//
	// +kubebuilder:validation:Optional
	PodName string `json:"podName,omitempty"`

	// SlurmState is the node's state as reported by Slurm.
	// Known values are: IDLE, ALLOCATED, MIXED, DOWN, DRAIN, POWER_UP, POWER_DOWN.
	//
	// +kubebuilder:validation:Optional
	SlurmState string `json:"slurmState,omitempty"`

	// LastTransitionTime is the last time the node transitioned to this phase.
	//
	// +kubebuilder:validation:Optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`

	// Message provides additional details about the node's state.
	// This is typically used to provide error messages.
	//
	// +kubebuilder:validation:Optional
	Message string `json:"message,omitempty"`
}

// NodePowerPhase represents the phase of a node's power state.
// It maps directly to corev1.PodPhase values for consistency.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Unknown
type NodePowerPhase corev1.PodPhase

const (
	// NodePowerPhasePending indicates the pod has been accepted by the system,
	// but one or more of the containers has not been started.
	NodePowerPhasePending NodePowerPhase = "Pending"

	// NodePowerPhaseRunning indicates the pod has been bound to a node
	// and all of the containers have been started.
	NodePowerPhaseRunning NodePowerPhase = "Running"

	// NodePowerPhaseSucceeded indicates that all containers in the pod
	// have voluntarily terminated with a container exit code of 0.
	NodePowerPhaseSucceeded NodePowerPhase = "Succeeded"

	// NodePowerPhaseFailed indicates that all containers in the pod have terminated,
	// and at least one container has terminated in a failure.
	NodePowerPhaseFailed NodePowerPhase = "Failed"

	// NodePowerPhaseUnknown indicates that for some reason the state of the pod
	// could not be obtained.
	NodePowerPhaseUnknown NodePowerPhase = "Unknown"
)

const (
	// KindNodeSetPowerState is the kind string for NodeSetPowerState resources.
	KindNodeSetPowerState = "NodeSetPowerState"

	// ConditionNodeSetPowerStateReady indicates that all active nodes are in the desired state.
	ConditionNodeSetPowerStateReady = "Ready"

	// ConditionNodeSetPowerStateScalingInProgress indicates that scaling operations are in progress.
	ConditionNodeSetPowerStateScalingInProgress = "ScalingInProgress"
)

func init() {
	SchemeBuilder.Register(&NodeSetPowerState{}, &NodeSetPowerStateList{})
}
