package consts

import corev1 "k8s.io/api/core/v1"

const (
	SlurmNodeDrain  = corev1.NodeConditionType("SlurmNodeDrain")
	SlurmNodeReboot = corev1.NodeConditionType("SlurmNodeReboot")
)

type ReasonConditionType string
type MessageConditionType string

const (
	ReasonDraining  ReasonConditionType = "Draining"
	ReasonDrained   ReasonConditionType = "Drained"
	ReasonRebooting ReasonConditionType = "Rebooting"
	ReasonRebooted  ReasonConditionType = "Rebooted"
)

const (
	MessageDraining  MessageConditionType = "Node is draining"
	MessageDrained   MessageConditionType = "Node has been drained"
	MessageRebooting MessageConditionType = "Node is rebooting"
	MessageRebooted  MessageConditionType = "Node has been rebooted"
)
