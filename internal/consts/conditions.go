package consts

import corev1 "k8s.io/api/core/v1"

const (
	SlurmNodeDrain  = corev1.NodeConditionType("SlurmNodeDrain")
	SlurmNodeReboot = corev1.NodeConditionType("SlurmNodeReboot")
)

type ReasonConditionType string
type MessageConditionType string

const (
	ReasonNodeDraining       ReasonConditionType = "NodeDraining"
	ReasonNodeDrained        ReasonConditionType = "NodeDrained"
	ReasonNodeUndrained      ReasonConditionType = "NodeUndrained"
	ReasonNodeRebooting      ReasonConditionType = "NodeRebooting"
	ReasonNodeRebooted       ReasonConditionType = "NodeRebooted"
	ReasonNodeNoRebootNeeded ReasonConditionType = "NodeNoRebootNeeded"
)

const (
	MessageDraining  MessageConditionType = "Node is draining"
	MessageDrained   MessageConditionType = "Node has been drained"
	MessageRebooting MessageConditionType = "Node is rebooting"
	MessageRebooted  MessageConditionType = "Node has been rebooted"
	MessageUndrained MessageConditionType = "Node has been undrained"
)
