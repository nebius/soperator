package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/consts"
)

// ActiveCheckSpec defines the desired state of ActiveCheck.
type ActiveCheckSpec struct {
	// Name defines the name of k8s cronJob
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// SlurmClusterRefName defines the name of Slurm Cluster
	// +kubebuilder:validation:Required
	SlurmClusterRefName string `json:"slurmClusterRefName"`

	// DependsOn specifies dependency on another active checks that should be completed
	// before running this one.
	// Name of another checks in the same namespace.
	// Note: All checks that the current one depends on must have runAfterCreation: true;
	// otherwise, the dependency relationship won’t be respected.
	// +kubebuilder:validation:Optional
	DependsOn []string `json:"dependsOn"`

	// Schedule defines the CronJob schedule.
	// By default, every year - at 00:00 on day-of-month 1 in January
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="0 0 1 1 *"
	Schedule string `json:"schedule,omitempty"`

	// Suspend indicates whether the action is suspended.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=true
	Suspend *bool `json:"suspend,omitempty"`

	// ActiveDeadlineSeconds defines the CronJob timeout in seconds
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=1800
	ActiveDeadlineSeconds int64 `json:"activeDeadlineSeconds,omitempty"`

	// SuccessfulJobsHistoryLimit defines the number of successful finished jobs to retain
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=3
	SuccessfulJobsHistoryLimit int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// FailedJobsHistoryLimit defines the number of failed finished jobs to retain
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=16
	FailedJobsHistoryLimit int32 `json:"failedJobsHistoryLimit,omitempty"`

	// RunAfterCreation specifies whether the job should run immediately after the CronJob is created.
	// +kubebuilder:default=true
	RunAfterCreation *bool `json:"runAfterCreation,omitempty"`

	// NodeSelector defines the desired selector for the K8s nodes to place Slurm workers on
	//
	// NOTE: NodeSelector could not be set if Affinity is specified
	//
	// +kubebuilder:validation:Optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Affinity defines the desired affinity for the K8s nodes to place Slurm workers on
	// NOTE: Affinity could not be set if NodeSelector is specified
	//
	// +kubebuilder:validation:Optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations define the desired tolerations for the K8s nodes to place Slurm workers on
	//
	// +kubebuilder:validation:Optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// PodTemplateNameRef points to a PodTemplate that contains the pod configuration.
	// +kubebuilder:validation:Optional
	PodTemplateNameRef *string `json:"podTemplateNameRef,omitempty"`

	// K8sJobSpec defines options for k8s cronjob
	// +kubebuilder:validation:Optional
	K8sJobSpec K8sJobSpec `json:"k8sJobSpec,omitempty"`

	// SlurmJobSpec defines options for k8s cronjob which creates slurm job
	// +kubebuilder:validation:Optional
	SlurmJobSpec SlurmJobSpec `json:"slurmJobSpec,omitempty"`

	// CheckType defines the type of the check
	// +kubebuilder:validation:Enum=k8sJob;slurmJob
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="k8sJob"
	CheckType string `json:"checkType,omitempty"`

	// SuccessReactions defines reaction on specific check when it succeeds
	// +kubebuilder:validation:Optional
	SuccessReactions *Reactions `json:"successReactions,omitempty"`

	// FailureReactions defines reaction on specific check when it fails
	// +kubebuilder:validation:Optional
	FailureReactions *Reactions `json:"failureReactions,omitempty"`
}

type Reactions struct {
	// DrainSlurmNode enabling slurm node draining
	// +kubebuilder:validation:Optional
	DrainSlurmNode *DrainSlurmNodeSpec `json:"drainSlurmNode,omitempty"`
	// CommentSlurmNode enabling slurm node commenting
	// +kubebuilder:validation:Optional
	CommentSlurmNode *bool `json:"commentSlurmNode,omitempty"`

	// AddReservation adds a slurm reservation with name "<prefix>-<nodeName>"
	// +kubebuilder:validation:Optional
	AddReservation *ReservationSpec `json:"addReservation,omitempty"`
	// RemoveReservation removs slurm reservation with name "<prefix>-<nodeName>"
	// +kubebuilder:validation:Optional
	RemoveReservation *ReservationSpec `json:"removeReservation,omitempty"`
}

type DrainSlurmNodeSpec struct {
	SetHardwareIssueSuspected bool `json:"setHardwareIssueSuspected,omitempty"`
}

type ReservationSpec struct {
	Prefix string `json:"prefix,omitempty"`
}

type ContainerSpec struct {
	Image        string               `json:"image,omitempty"`
	Command      []string             `json:"command,omitempty"`
	Args         []string             `json:"args,omitempty"`
	Env          []corev1.EnvVar      `json:"env,omitempty"`
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
	Volumes      []corev1.Volume      `json:"volumes,omitempty"`
	// AppArmorProfile defines the AppArmor profile for the containers
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="unconfined"
	AppArmorProfile string `json:"appArmorProfile,omitempty"`
}
type K8sJobSpec struct {
	JobContainer ContainerSpec `json:"jobContainer,omitempty"`
	// ScriptRefName name of configMap with custom script. Data expected in the key script.sh inside ConfigMap.
	// +kubebuilder:validation:Optional
	ScriptRefName *string `json:"scriptRefName,omitempty"`
}

type SlurmJobSpec struct {
	JobContainer   ContainerSpec `json:"jobContainer,omitempty"`
	MungeContainer ContainerSpec `json:"mungeContainer,omitempty"`
	// SbatchScriptRefName name of configMap with sbatch script. Data expected in the key sbatch.sh inside ConfigMap.
	// +kubebuilder:validation:Optional
	SbatchScriptRefName *string `json:"sbatchScriptRefName,omitempty"`
	// Multiline sbatch script
	// +kubebuilder:validation:Optional
	SbatchScript *string `json:"sbatchScript,omitempty"`
	// Run sbatch script on each worker exactly once using job array
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	EachWorkerJobArray bool `json:"eachWorkerJobArray,omitempty"`
	// Run sbatch script on each worker exactly once using separate jobs
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	EachWorkerJobs bool `json:"eachWorkerJobs,omitempty"`
	// MaxNumberOfJobs defines the maximum number of simultaneously running jobs.
	// If it's less than number of workers random MaxNumberOfJobs workers will be chosen.
	// If MaxNumberOfJobs equals 0 there is no limitation.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=0
	MaxNumberOfJobs *int64 `json:"maxNumberOfJobs,omitempty"`
}

// ActiveCheckK8sJobsStatus defines the observed state of ActiveCheck k8s jobs.
type ActiveCheckK8sJobsStatus struct {
	// +kubebuilder:validation:Optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// +kubebuilder:validation:Optional
	LastJobScheduleTime *metav1.Time `json:"lastJobScheduleTime"`
	// +kubebuilder:validation:Optional
	LastJobSuccessfulTime *metav1.Time `json:"lastJobSuccessfulTime"`

	LastJobName   string                         `json:"lastJobName"`
	LastJobStatus consts.ActiveCheckK8sJobStatus `json:"lastJobStatus"`
}

// ActiveCheckSlurmJobsStatus defines the observed state of ActiveCheck slurm jobs.
type ActiveCheckSlurmJobsStatus struct {
	// +kubebuilder:validation:Optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// +kubebuilder:validation:Optional
	LastRunId string `json:"lastRunId"`
	// +kubebuilder:validation:Optional
	LastRunName string `json:"lastRunName"`
	// +kubebuilder:validation:Optional
	LastRunStatus consts.ActiveCheckSlurmRunStatus `json:"lastRunStatus"`
	// +kubebuilder:validation:Optional
	LastRunFailJobsAndReasons []JobAndReason `json:"lastRunFailJobsAndReasons"`
	// +kubebuilder:validation:Optional
	LastRunErrorJobsAndReasons []JobAndReason `json:"lastRunErrorJobsAndReasons"`
	// +kubebuilder:validation:Optional
	LastRunSubmitTime *metav1.Time `json:"lastRunSubmitTime"`
}

// ActiveCheckStatus defines the observed state of ActiveCheck.
type ActiveCheckStatus struct {
	StatusMetadata  `json:",inline"`
	K8sJobsStatus   ActiveCheckK8sJobsStatus   `json:"k8sJobsStatus,omitempty"`
	SlurmJobsStatus ActiveCheckSlurmJobsStatus `json:"slurmJobsStatus,omitempty"`
}

type JobAndReason struct {
	JobID  string `json:"jobID"`
	Reason string `json:"reason"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.checkType`,description="Active check type"
// +kubebuilder:printcolumn:name="Run After Creation",type=boolean,JSONPath=`.spec.runAfterCreation`,description="Whether to run this check after creation"
// +kubebuilder:printcolumn:name="Suspend Periodic",type=boolean,JSONPath=`.spec.suspend`,description="Whether to suspend periodic runs of this check"
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`,description="Schedule"
// +kubebuilder:printcolumn:name="K8s Status",type=string,JSONPath=`.status.k8sJobsStatus.lastJobStatus`,description="Status of the last K8s job"
// +kubebuilder:printcolumn:name="Slurm Status",type=string,JSONPath=`.status.slurmJobsStatus.lastRunStatus`,description="Status of the last Slurm job"
// +kubebuilder:printcolumn:name="Slurm Submit Time",type=string,JSONPath=`.status.slurmJobStatus.lastJobSubmitTime`,description="Submission time of the last Slurm job"
// +kubebuilder:printcolumn:name="Slurm ID",type=string,JSONPath=`.status.slurmJobsStatus.lastRunId`,description="ID of the last Slurm job"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="When the job was created"

// ActiveCheck is the Schema for the activechecks API.
type ActiveCheck struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ActiveCheckSpec   `json:"spec,omitempty"`
	Status ActiveCheckStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ActiveCheckList contains a list of ActiveCheck.
type ActiveCheckList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ActiveCheck `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ActiveCheck{}, &ActiveCheckList{})
}

// SetDefaults sets default values for ActiveCheckSpec
func (s *ActiveCheckSpec) SetDefaults() {
	if s.Suspend == nil {
		s.Suspend = ptr.To(true)
	}
	if s.RunAfterCreation == nil {
		s.RunAfterCreation = ptr.To(true)
	}
}
