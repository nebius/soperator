package kubeobjects

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SlurmClusterPhaseAvailable = "Available"

	SlurmClusterConditionCommonAvailable            = "CommonAvailable"
	SlurmClusterConditionControllersAvailable       = "ControllersAvailable"
	SlurmClusterConditionLoginAvailable             = "LoginAvailable"
	SlurmClusterConditionAccountingAvailable        = "AccountingAvailable"
	SlurmClusterConditionSConfigControllerAvailable = "SConfigControllerAvailable"

	NodeSetPhaseReady = "Ready"

	ActiveCheckTypeK8sJob   = "k8sJob"
	ActiveCheckTypeSlurmJob = "slurmJob"

	ActiveCheckK8sJobStatusComplete = "Complete"

	ActiveCheckSlurmRunStatusComplete  = "Complete"
	ActiveCheckSlurmRunStatusFailed    = "Failed"
	ActiveCheckSlurmRunStatusCancelled = "Cancelled"
	ActiveCheckSlurmRunStatusError     = "Error"
	ActiveCheckSlurmRunStatusSkipped   = "Skipped"
)

type ObjectMeta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Generation  int64             `json:"generation"`
	Annotations map[string]string `json:"annotations"`
}

type SlurmClusterList struct {
	Items []SlurmCluster `json:"items"`
}

type SlurmCluster struct {
	Metadata ObjectMeta         `json:"metadata"`
	Spec     SlurmClusterSpec   `json:"spec"`
	Status   SlurmClusterStatus `json:"status"`
}

type SlurmClusterSpec struct {
	Topology               *SlurmClusterTopology  `json:"topology"`
	PartitionConfiguration PartitionConfiguration `json:"partitionConfiguration"`
}

type SlurmClusterTopology struct {
	Topologies []NamedTopology `json:"topologies"`
}

type NamedTopology struct {
	Name           string         `json:"name"`
	ClusterDefault *bool          `json:"clusterDefault"`
	Topo           TopologyPlugin `json:"topo"`
	NodeSetRefs    []string       `json:"nodeSetRefs"`
}

type TopologyPlugin struct {
	Type       string `json:"type"`
	BlockSizes []int  `json:"blockSizes"`
}

type PartitionConfiguration struct {
	ConfigType string      `json:"configType"`
	Partitions []Partition `json:"partitions"`
}

type Partition struct {
	Name        string `json:"name"`
	TopologyRef string `json:"topologyRef"`
}

type SlurmClusterStatus struct {
	Phase      *string            `json:"phase"`
	Conditions []metav1.Condition `json:"conditions"`
}

type NodeSetList struct {
	Items []NodeSet `json:"items"`
}

type NodeSet struct {
	Metadata ObjectMeta    `json:"metadata"`
	Spec     NodeSetSpec   `json:"spec"`
	Status   NodeSetStatus `json:"status"`
}

type NodeSetSpec struct {
	ClusterName                 string         `json:"clusterName"`
	Replicas                    int32          `json:"replicas"`
	EphemeralNodes              *bool          `json:"ephemeralNodes"`
	InitialNumberEphemeralNodes int32          `json:"initialNumberEphemeralNodes"`
	GPU                         NodeSetGPUSpec `json:"gpu"`
}

type NodeSetGPUSpec struct {
	Enabled bool `json:"enabled"`
}

type NodeSetStatus struct {
	Phase      string             `json:"phase"`
	Replicas   int32              `json:"replicas"`
	Conditions []metav1.Condition `json:"conditions"`
}

type NodeSetPowerState struct {
	Spec NodeSetPowerStateSpec `json:"spec"`
}

type NodeSetPowerStateSpec struct {
	ActiveNodes []int32 `json:"activeNodes"`
}

type KruiseStatefulSet struct {
	Spec KruiseStatefulSetSpec `json:"spec"`
}

type KruiseStatefulSetSpec struct {
	Replicas        *int32            `json:"replicas"`
	ReserveOrdinals []json.RawMessage `json:"reserveOrdinals"`
}

type ActiveCheckList struct {
	Items []ActiveCheck `json:"items"`
}

type ActiveCheck struct {
	Metadata ObjectMeta        `json:"metadata"`
	Spec     ActiveCheckSpec   `json:"spec"`
	Status   ActiveCheckStatus `json:"status"`
}

type ActiveCheckSpec struct {
	RunAfterCreation *bool  `json:"runAfterCreation"`
	CheckType        string `json:"checkType"`
}

type ActiveCheckStatus struct {
	K8sJobsStatus   ActiveCheckK8sJobsStatus   `json:"k8sJobsStatus"`
	SlurmJobsStatus ActiveCheckSlurmJobsStatus `json:"slurmJobsStatus"`
}

type ActiveCheckK8sJobsStatus struct {
	LastJobStatus string `json:"lastJobStatus"`
}

type ActiveCheckSlurmJobsStatus struct {
	LastRunID                  string   `json:"lastRunId"`
	LastRunStatus              string   `json:"lastRunStatus"`
	LastRunFailJobsAndReasons  []any    `json:"lastRunFailJobsAndReasons"`
	LastRunErrorJobsAndReasons []any    `json:"lastRunErrorJobsAndReasons"`
	LastRunCancelledJobs       []string `json:"lastRunCancelledJobs"`
}

type JailedConfig struct {
	Metadata ObjectMeta         `json:"metadata"`
	Spec     JailedConfigSpec   `json:"spec"`
	Status   JailedConfigStatus `json:"status"`
}

type JailedConfigSpec struct {
	UpdateActions []string `json:"updateActions"`
}

type JailedConfigStatus struct {
	AppliedHash string             `json:"appliedHash"`
	Conditions  []metav1.Condition `json:"conditions"`
}
