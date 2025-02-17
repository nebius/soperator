package consts

import corev1 "k8s.io/api/core/v1"

const (
	SlurmNodeDrain  corev1.NodeConditionType = "SlurmNodeDrain"
	SlurmNodeReboot corev1.NodeConditionType = "SlurmNodeReboot"

	K8SNodeDegraded             corev1.NodeConditionType = "NodeDegraded"
	K8SNodeMaintenanceScheduled corev1.NodeConditionType = "MaintenanceScheduled"
)

type ReasonConditionType string
type MessageConditionType string

const (
	ReasonNodeNeedDrain      ReasonConditionType = "NodeNeedDrain"
	ReasonNodeDraining       ReasonConditionType = "NodeDraining"
	ReasonNodeDrained        ReasonConditionType = "NodeDrained"
	ReasonNodeUndrained      ReasonConditionType = "NodeUndrained"
	ReasonNodeNeedReboot     ReasonConditionType = "NodeNeedReboot"
	ReasonNodeRebooting      ReasonConditionType = "NodeRebooting"
	ReasonNodeRebooted       ReasonConditionType = "NodeRebooted"
	ReasonNodeNoRebootNeeded ReasonConditionType = "NodeNoRebootNeeded"

	ReasonSlurmNodeDegraded ReasonConditionType = "SlurmNodeDegraded"
)

const (
	MessageDraining  MessageConditionType = "Node is draining"
	MessageDrained   MessageConditionType = "Node has been drained"
	MessageRebooting MessageConditionType = "Node is rebooting"
	MessageRebooted  MessageConditionType = "Node has been rebooted"
	MessageUndrained MessageConditionType = "Node has been undrained"

	MessageSlurmNodeDegraded    MessageConditionType = "Some slurm nodes on the k8s nod are degraded"
	MessageMaintenanceScheduled MessageConditionType = "Maintenance is scheduled on k8s node"
	MessageNodeIsRebooted       MessageConditionType = "Node is rebooted"
)
