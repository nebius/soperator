package kubeobjects

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type SlurmClusterList struct {
	Items []SlurmCluster `json:"items"`
}

type SlurmCluster struct {
	Metadata ObjectMeta         `json:"metadata"`
	Status   SlurmClusterStatus `json:"status"`
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
	ClusterName string         `json:"clusterName"`
	Replicas    int32          `json:"replicas"`
	GPU         NodeSetGPUSpec `json:"gpu"`
}

type NodeSetGPUSpec struct {
	Enabled bool `json:"enabled"`
}

type NodeSetStatus struct {
	Phase    string `json:"phase"`
	Replicas int32  `json:"replicas"`
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
