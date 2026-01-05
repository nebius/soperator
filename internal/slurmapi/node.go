package slurmapi

import (
	"errors"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
)

type Node struct {
	Name        string
	ClusterName string
	InstanceID  string
	States      map[api.V0041NodeState]struct{}
	Reason      *NodeReason
	Partitions  []string
	Tres        string    // Trackable Resources (e.g., CPUs, GPUs) assigned to the node.
	Address     string    // IP Address of the node in the Kubernetes cluster.
	BootTime    time.Time // The boot time of the node.
	Comment     string
	Reservation string

	// Resource-related fields (nullable to detect missing data)
	CPUs                *int32
	AllocCPUs           *int32
	AllocIdleCPUs       *int32
	EffectiveCPUs       *int32
	AllocMemoryMB       *int64
	RealMemoryMB        *int64
	FreeMemoryMB        *int64
	SpecializedMemoryMB *int64
}

type NodeReason struct {
	Reason         string
	OriginalReason string
	ChangedAt      time.Time
}

func validateAPINode(node api.V0041Node) error {
	var errs []error

	if node.State == nil || len(*node.State) == 0 {
		errs = append(errs, errors.New("node doesn't have any state"))
	}

	if node.Name == nil {
		errs = append(errs, errors.New("node doesn't have name"))
	}

	if node.ClusterName == nil {
		errs = append(errs, errors.New("node doesn't have cluster name"))
	}

	if node.InstanceId == nil {
		errs = append(errs, errors.New("node doesn't have instance id"))
	}

	if node.Reason != nil && len(*node.Reason) != 0 && (node.ReasonChangedAt == nil || node.ReasonChangedAt.Number == nil) {
		errs = append(errs, errors.New("node doesn't have reasonChangedAt or reasonChangedAt.number, but has reason"))
	}

	return errors.Join(errs...)
}

func NodeFromAPI(node api.V0041Node) (Node, error) {
	if err := validateAPINode(node); err != nil {
		return Node{}, err
	}

	var res Node

	nodeStates := make(map[api.V0041NodeState]struct{}, len(*node.State))
	for _, state := range *node.State {
		nodeStates[state] = struct{}{}
	}

	res = Node{
		Name:        *node.Name,
		ClusterName: *node.ClusterName,
		InstanceID:  *node.InstanceId,
		States:      nodeStates,
		Partitions:  *node.Partitions,
		Tres:        *node.Tres,
		Address:     *node.Address,
		Comment:     *node.Comment,
	}

	res.CPUs = node.Cpus
	res.AllocCPUs = node.AllocCpus
	res.AllocIdleCPUs = node.AllocIdleCpus
	res.EffectiveCPUs = node.EffectiveCpus
	res.AllocMemoryMB = node.AllocMemory
	res.RealMemoryMB = node.RealMemory
	res.FreeMemoryMB = convertUint64Struct(node.FreeMem)
	res.SpecializedMemoryMB = node.SpecializedMemory

	if node.BootTime != nil && node.BootTime.Number != nil {
		res.BootTime = time.Unix(*node.BootTime.Number, 0)
	}

	if node.Reason != nil && len(*node.Reason) != 0 {
		res.Reason = &NodeReason{
			Reason:    *node.Reason,
			ChangedAt: time.Unix(*node.ReasonChangedAt.Number, 0),
		}
	}

	if node.Reservation != nil {
		res.Reservation = *node.Reservation
	}

	return res, nil
}

func (n *Node) IsIdleDrained() bool {
	_, drained := n.States[api.V0041NodeStateDRAIN]
	_, idle := n.States[api.V0041NodeStateIDLE]

	return drained && idle
}

func (n *Node) IsDrainState() bool {
	_, exists := n.States[api.V0041NodeStateDRAIN]
	return exists
}

func (n *Node) IsCompletingState() bool {
	_, exists := n.States[api.V0041NodeStateCOMPLETING]
	return exists
}

func (n *Node) IsMaintenanceState() bool {
	_, exists := n.States[api.V0041NodeStateMAINTENANCE]
	return exists
}

func (n *Node) IsReservedState() bool {
	_, exists := n.States[api.V0041NodeStateRESERVED]
	return exists
}

func (n *Node) IsFailState() bool {
	_, exists := n.States[api.V0041NodeStateFAIL]
	return exists
}

func (n *Node) IsPlannedState() bool {
	_, exists := n.States[api.V0041NodeStatePLANNED]
	return exists
}

func (n *Node) IsDownState() bool {
	_, exists := n.States[api.V0041NodeStateDOWN]
	return exists
}

// baseStates defines the mutually exclusive base states of a Slurm node.
// The node state is a 32-bit integer where the lowest 4 bits (0x0000000f) encode
// exactly 6 mutually exclusive base states: IDLE, DOWN, ALLOCATED, ERROR, MIXED, UNKNOWN.
// These are the only states that can be used as the "base" state of a node.
//
// Additional states like COMPLETING, DRAIN, MAINTENANCE, RESERVED, FAIL, PLANNED are
// flag bits that can be combined with base states. For example, a node can be
// IDLE+COMPLETING simultaneously (e.g., State=IDLE+COMPLETING+DYNAMIC_NORM+NOT_RESPONDING).
//
// More details: https://github.com/SchedMD/slurm/blob/master/slurm/slurm.h.in
var baseStates = []api.V0041NodeState{
	api.V0041NodeStateUNKNOWN,
	api.V0041NodeStateDOWN,
	api.V0041NodeStateIDLE,
	api.V0041NodeStateALLOCATED,
	api.V0041NodeStateERROR,
	api.V0041NodeStateMIXED,
}

// BaseState returns the base state of the node.
// Slurm node has one base state and multiple additional states (flag bits).
// The base state is one of: UNKNOWN, DOWN, IDLE, ALLOCATED, ERROR, MIXED.
// Additional flag bits like COMPLETING, DRAIN, MAINTENANCE, etc. can be combined
// with the base state to form the complete node state.
//
// More details: https://github.com/SchedMD/slurm/blob/1cb50f245f05d851f2383e326db2f20a01820a88/slurm/slurm.h#L961
func (n *Node) BaseState() api.V0041NodeState {
	for _, baseState := range baseStates {
		if _, ok := n.States[baseState]; ok {
			return baseState
		}
	}
	return ""
}

func valueOrDefault(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func convertUint64Struct(input *api.V0041Uint64NoValStruct) *int64 {
	if input == nil || input.Set == nil || !*input.Set || input.Number == nil {
		return nil
	}
	return input.Number
}

// CPUTotal returns the total number of CPUs on the node.
// First tries to use the CPUs field, then falls back to parsing Tres.
func (n *Node) CPUTotal() (float64, bool) {
	if n.CPUs != nil {
		return float64(*n.CPUs), true
	}
	if tres, err := ParseTrackableResources(n.Tres); err == nil && tres.CPUCount > 0 {
		return float64(tres.CPUCount), true
	}
	return 0, false
}

// CPUAllocated returns the number of CPUs currently allocated on the node.
func (n *Node) CPUAllocated() (float64, bool) {
	if n.AllocCPUs == nil {
		return 0, false
	}
	return float64(*n.AllocCPUs), true
}

// CPUIdle returns the number of idle CPUs on the node.
func (n *Node) CPUIdle() (float64, bool) {
	if n.AllocIdleCPUs == nil {
		return 0, false
	}
	return float64(*n.AllocIdleCPUs), true
}

// CPUEffective returns the effective number of CPUs on the node.
func (n *Node) CPUEffective() (float64, bool) {
	if n.EffectiveCPUs == nil {
		return 0, false
	}
	return float64(*n.EffectiveCPUs), true
}

// MemoryTotalBytes returns the total memory on the node in bytes.
// First tries to use the RealMemoryMB field, then falls back to parsing Tres.
func (n *Node) MemoryTotalBytes() (float64, bool) {
	if n.RealMemoryMB != nil {
		return mbToBytes(*n.RealMemoryMB), true
	}
	if tres, err := ParseTrackableResources(n.Tres); err == nil && tres.MemoryBytes > 0 {
		return float64(tres.MemoryBytes), true
	}
	return 0, false
}

// MemoryAllocatedBytes returns the allocated memory on the node in bytes.
func (n *Node) MemoryAllocatedBytes() (float64, bool) {
	if n.AllocMemoryMB == nil {
		return 0, false
	}
	return mbToBytes(*n.AllocMemoryMB), true
}

// MemoryFreeBytes returns the free memory on the node in bytes.
func (n *Node) MemoryFreeBytes() (float64, bool) {
	if n.FreeMemoryMB == nil {
		return 0, false
	}
	return mbToBytes(*n.FreeMemoryMB), true
}

// MemoryEffectiveBytes returns the effective memory on the node in bytes.
// Effective memory is total memory minus memory reserved for daemons (specialized memory), if available.
func (n *Node) MemoryEffectiveBytes() (float64, bool) {
	if n.RealMemoryMB == nil {
		if tres, err := ParseTrackableResources(n.Tres); err == nil && tres.MemoryBytes > 0 {
			return float64(tres.MemoryBytes), true
		}
		return 0, false
	}

	effectiveMB := *n.RealMemoryMB
	if n.SpecializedMemoryMB != nil {
		effectiveMB -= *n.SpecializedMemoryMB
		if effectiveMB < 0 {
			effectiveMB = 0
		}
	}
	return mbToBytes(effectiveMB), true
}

// mbToBytes converts megabytes to bytes.
func mbToBytes(mb int64) float64 {
	return float64(mb) * 1024 * 1024
}
