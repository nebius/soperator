package slurmapi

import (
	"errors"
	"time"

	slurmapispec "github.com/SlinkyProject/slurm-client/api/v0041"
)

type Node struct {
	Name        string
	ClusterName string
	InstanceID  string
	States      map[slurmapispec.V0041NodeState]struct{}
	Reason      *NodeReason
	Partitions  []string
	Tres        string    // Trackable Resources (e.g., CPUs, GPUs) assigned to the node.
	Address     string    // IP Address of the node in the Kubernetes cluster.
	BootTime    time.Time // The boot time of the node.
}

type NodeReason struct {
	Reason    string
	ChangedAt time.Time
}

func validateAPINode(node slurmapispec.V0041Node) error {
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

func NodeFromAPI(node slurmapispec.V0041Node) (Node, error) {
	if err := validateAPINode(node); err != nil {
		return Node{}, err
	}

	var res Node

	nodeStates := make(map[slurmapispec.V0041NodeState]struct{}, len(*node.State))
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
	}

	if node.BootTime != nil && node.BootTime.Number != nil {
		res.BootTime = time.Unix(*node.BootTime.Number, 0)
	}

	if node.Reason != nil && len(*node.Reason) != 0 {
		res.Reason = &NodeReason{
			Reason:    *node.Reason,
			ChangedAt: time.Unix(*node.ReasonChangedAt.Number, 0),
		}
	}

	return res, nil
}

func (n *Node) IsIdleDrained() bool {
	_, drained := n.States[slurmapispec.V0041NodeStateDRAIN]
	_, idle := n.States[slurmapispec.V0041NodeStateIDLE]

	return drained && idle
}

func (n *Node) IsDrainState() bool {
	_, exists := n.States[slurmapispec.V0041NodeStateDRAIN]
	return exists
}

func (n *Node) IsMaintenanceState() bool {
	_, exists := n.States[slurmapispec.V0041NodeStateMAINTENANCE]
	return exists
}

func (n *Node) IsReservedState() bool {
	_, exists := n.States[slurmapispec.V0041NodeStateRESERVED]
	return exists
}

func (n *Node) IsDownState() bool {
	_, exists := n.States[slurmapispec.V0041NodeStateDOWN]
	return exists
}

var baseStates = []slurmapispec.V0041NodeState{
	slurmapispec.V0041NodeStateUNKNOWN,
	slurmapispec.V0041NodeStateDOWN,
	slurmapispec.V0041NodeStateIDLE,
	slurmapispec.V0041NodeStateALLOCATED,
	slurmapispec.V0041NodeStateERROR,
	slurmapispec.V0041NodeStateMIXED,
}

// BaseState returns the base state of the node.
// Slurm node has one base state and multiple additional states.
// More details: https://github.com/SchedMD/slurm/blob/1cb50f245f05d851f2383e326db2f20a01820a88/slurm/slurm.h#L961
func (n *Node) BaseState() slurmapispec.V0041NodeState {
	for _, baseState := range baseStates {
		if _, ok := n.States[baseState]; ok {
			return baseState
		}
	}
	return ""
}
