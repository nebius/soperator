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
