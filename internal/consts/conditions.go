package consts

import corev1 "k8s.io/api/core/v1"

const (
	SlurmNodeDrain  corev1.NodeConditionType = "SlurmNodeDrain"
	SlurmNodeReboot corev1.NodeConditionType = "SlurmNodeReboot"

	// SoperatorChecks related conditions to distinguish what happened to the k8s node.
	SoperatorChecksK8SNodeDegraded    corev1.NodeConditionType = "SoperatorChecksNodeDegraded"
	SoperatorChecksK8SNodeMaintenance corev1.NodeConditionType = "SoperatorChecksNodeMaintenance"
	HardwareIssuesSuspected           corev1.NodeConditionType = "HardwareIssuesSuspected"

	DefaultMaintenanceConditionType corev1.NodeConditionType = "NebiusMaintenanceScheduled"
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

	ReasonNodeNotReady ReasonConditionType = "NodeNotReady"

	ReasonGPUHealthCheckFailed ReasonConditionType = "GPUHealthCheckFailed (Soperator)"
)

const (
	MessageDraining  MessageConditionType = "Node is draining"
	MessageDrained   MessageConditionType = "Node has been drained"
	MessageRebooting MessageConditionType = "Node is rebooting"
	MessageRebooted  MessageConditionType = "Node has been rebooted"
	MessageUndrained MessageConditionType = "Node has been undrained"

	MessageSlurmNodeDegraded       MessageConditionType = "Some slurm nodes on the k8s nod are degraded"
	MessageMaintenanceScheduled    MessageConditionType = "Maintenance is scheduled on k8s node"
	MessageHardwareIssuesSuspected MessageConditionType = "Hardware issues suspected on k8s node"
	MessageNodeIsRebooted          MessageConditionType = "Node is rebooted"

	MessageNodeIsNotReady MessageConditionType = "Node is NotReady for more than 5 minutes, undrain before deletion"
)

// ActiveCheckK8sJobStatus defines status for ActiveCheck k8s job.
type ActiveCheckK8sJobStatus string

const (
	ActiveCheckK8sJobStatusActive    ActiveCheckK8sJobStatus = "Active"
	ActiveCheckK8sJobStatusPending   ActiveCheckK8sJobStatus = "Pending"
	ActiveCheckK8sJobStatusComplete  ActiveCheckK8sJobStatus = "Complete"
	ActiveCheckK8sJobStatusFailed    ActiveCheckK8sJobStatus = "Failed"
	ActiveCheckK8sJobStatusSuspended ActiveCheckK8sJobStatus = "Suspended"
	ActiveCheckK8sJobStatusUnknown   ActiveCheckK8sJobStatus = "Unknown"
)

// ActiveCheckSlurmRunStatus defines status for ActiveCheck slurm job.
type ActiveCheckSlurmRunStatus string

const (
	ActiveCheckSlurmRunStatusInProgress ActiveCheckSlurmRunStatus = "InProgress"
	ActiveCheckSlurmRunStatusComplete   ActiveCheckSlurmRunStatus = "Complete"
	ActiveCheckSlurmRunStatusFailed     ActiveCheckSlurmRunStatus = "Failed"
	ActiveCheckSlurmRunStatusError      ActiveCheckSlurmRunStatus = "Error"
)
