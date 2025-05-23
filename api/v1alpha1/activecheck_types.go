package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

	// Schedule defines the CronJob schedule.
	// By default, every year - at 00:00 on day-of-month 1 in January
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="0 0 1 1 *"
	Schedule string `json:"schedule,omitempty"`

	// Suspend indicates whether the action is suspended.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=true
	Suspend bool `json:"suspend,omitempty"`

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
	RunAfterCreation bool `json:"runAfterCreation,omitempty"`

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

	// Reactions defines reaction on specific check
	// +kubebuilder:validation:Optional
	Reactions Reactions `json:"reactions,omitempty"`
}

type Reactions struct {
	// SetCondition enabling setting condition to the k8s node
	// +kubebuilder:validation:Optional
	SetCondition bool `json:"setCondition,omitempty"`

	// DrainSlurmNode enabling slurm node draining if check failed
	// +kubebuilder:validation:Optional
	DrainSlurmNode bool `json:"DrainSlurmNode,omitempty"`
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
	LastJobId *int32 `json:"lastJobId"`
	// +kubebuilder:validation:Optional
	LastJobName *string `json:"lastJobName"`
	// +kubebuilder:validation:Optional
	LastJobState string `json:"lastJobState"`
	// +kubebuilder:validation:Optional
	LastJobStateReason *string `json:"lastJobStateReason"`
	// +kubebuilder:validation:Optional
	LastJobSubmitTime *metav1.Time `json:"lastJobSubmitTime"`
	// +kubebuilder:validation:Optional
	LastJobStartTime *metav1.Time `json:"lastJobStartTime"`
	// +kubebuilder:validation:Optional
	LastJobEndTime *metav1.Time `json:"lastJobEndTime"`
}

// ActiveCheckStatus defines the observed state of ActiveCheck.
type ActiveCheckStatus struct {
	StatusMetadata  `json:",inline"`
	K8sJobsStatus   ActiveCheckK8sJobsStatus   `json:"k8sJobsStatus,omitempty"`
	SlurmJobsStatus ActiveCheckSlurmJobsStatus `json:"slurmJobsStatus,omitempty"`

	// +kubebuilder:default=false
	ServiceAccountReady bool `json:"serviceAccountReady,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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
