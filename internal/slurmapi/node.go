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
	TresUsed    string    // Trackable Resources currently allocated for jobs.
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
		TresUsed:    valueOrDefault(node.TresUsed),
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

var baseStates = []api.V0041NodeState{
	api.V0041NodeStateUNKNOWN,
	api.V0041NodeStateDOWN,
	api.V0041NodeStateIDLE,
	api.V0041NodeStateALLOCATED,
	api.V0041NodeStateERROR,
	api.V0041NodeStateMIXED,
	api.V0041NodeStateCOMPLETING,
}

// BaseState returns the base state of the node.
// Slurm node has one base state and multiple additional states.
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
