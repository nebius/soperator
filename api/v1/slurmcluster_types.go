/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SlurmClusterSpec defines the desired state of SlurmCluster
type SlurmClusterSpec struct {
	// CRVersion defines the version of the Operator the Custom Resource belongs to
	//
	// +kubebuilder:validation:Optional
	CRVersion string `json:"crVersion,omitempty"` // TODO backward compatibility

	// Pause set to true gracefully stops the cluster.
	// Setting it to false after shut down starts the cluster back
	//
	// +kubebuilder:validation:Optional
	Pause bool `json:"pause,omitempty"` // TODO cluster pausing/resuming

	// PeriodicChecks defines k8s slurm specific cronjobs
	//
	// +kubebuilder:validation:Optional
	PeriodicChecks PeriodicChecks `json:"periodicChecks"`

	// K8sNodeFilters define the k8s node filters used further in Slurm node specifications
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	K8sNodeFilters []K8sNodeFilter `json:"k8sNodeFilters"`

	// VolumeSources define the sources for the volumes
	//
	// +kubebuilder:validation:Optional
	VolumeSources []VolumeSource `json:"volumeSources,omitempty"`

	// Secrets defines the [corev1.Secret] references required for Slurm cluster
	//
	// +kubebuilder:validation:Required
	Secrets Secrets `json:"secrets"`

	// SlurmNodes defines the desired state of Slurm nodes
	//
	// +kubebuilder:validation:Required
	SlurmNodes SlurmNodes `json:"slurmNodes"`
}

// PeriodicChecks defines k8s slurm specific cronjobs
// +kubebuilder:validation:Optional
type PeriodicChecks struct {
	NCCLBenchmark NCCLBenchmark `json:"ncclBenchmark"`
}

// NCCLBenchmark slurm nccl-test benchmark
type NCCLBenchmark struct {
	// Name defines the name of the NCCLBenchmark
	//
	// +kubebuilder:default:="nccl-benchmark"
	Name string `json:"name"`

	// Suspend set to true stops cronjobs scheduling.
	//
	// +kubebuilder:default:=false
	Suspend bool `json:"suspend,omitempty"`

	// Cronjob schedule. By default run benchmark every 3 hours
	//
	// +kubebuilder:default:="0 */3 * * *"
	Schedule string `json:"schedule"`

	// ActiveDeadlineSeconds is a k8s CronJob timeout seconds
	// +kubebuilder:default:=1800
	ActiveDeadlineSeconds int64 `json:"activeDeadlineSeconds"`

	// SuccessfulJobsHistoryLimit The number of successful finished jobs to retain
	// +kubebuilder:default:=3
	SuccessfulJobsHistoryLimit int32 `json:"successfulJobsHistoryLimit"`

	// FailedJobsHistoryLimit The number of failed finished jobs to retain
	// +kubebuilder:default:=3
	FailedJobsHistoryLimit int32 `json:"failedJobsHistoryLimit"`

	// Image defines the container image
	//
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// NcclSettings defines nccl test params
	// +kubebuilder:validation:Optional
	NcclSettings NcclSettings `json:"ncclSettings"`

	// FailureActions defines actions if benchmark failed
	// +kubebuilder:validation:Optional
	FailureActions FailureActions `json:"failureActions"`

	// K8sNodeFilterName defines the Kubernetes node filter name associated with the Slurm node.
	// Must correspond to the name of one of [K8sNodeFilter]
	//
	// +kubebuilder:validation:Required
	K8sNodeFilterName string `json:"k8sNodeFilterName"`
}

type NcclSettings struct {
	// MinBytes minimum size to start with
	// +kubebuilder:default:="512Mb"
	MinBytes string `json:"minBytes"`

	// MaxBytes maximum size to end at
	// +kubebuilder:default:="8Gb"
	MaxBytes string `json:"maxBytes"`

	// StepFactor multiplication factor between sizes
	// +kubebuilder:default:="2"
	StepFactor string `json:"stepFactor"`

	// Timeout in nccl test format string
	// +kubebuilder:default:="20:00"
	Timeout string `json:"timeout"`

	// ThresholdMoreThan fail cronjob if result less than value
	// +kubebuilder:default:="0"
	ThresholdMoreThan string `json:"thresholdMoreThan"`
}

type FailureActions struct {

	// SetSlurmNodeDrainState drain slurm node if benchmark failed
	// +kubebuilder:default:="false"
	SetSlurmNodeDrainState string `json:"setSlurmNodeDrainState"`
}

// K8sNodeFilter defines the k8s node filter used in Slurm node specifications
type K8sNodeFilter struct {
	// Name defines the name of the filter
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Affinity defines the desired affinity for the node
	//
	// NOTE: Affinity could not be set if NodeSelector is specified
	//
	// +kubebuilder:validation:Optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations define the desired tolerations for the node
	//
	// +kubebuilder:validation:Optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// NodeSelector defines the desired selector for the node
	//
	// NOTE: NodeSelector could not be set if Affinity is specified
	//
	// +kubebuilder:validation:Optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// VolumeSource defines the source for the volume
type VolumeSource struct {
	corev1.VolumeSource `json:",inline"`

	// Name defines the name of the volume source
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// Secrets defines the [corev1.Secret] references required for Slurm cluster
type Secrets struct {
	// MungeKey defines the [corev1.Secret] reference required for inter-server communication of Slurm nodes
	//
	// +kubebuilder:validation:Required
	MungeKey MungeKeySecret `json:"mungeKey"`

	// SSHRootPublicKeys defines the [corev1.Secret] reference required for SSH connection to Slurm login nodes.
	// Required in case of login node usage
	//
	// +kubebuilder:validation:Optional
	SSHRootPublicKeys *SSHPublicKeysSecret `json:"sshRootPublicKeys,omitempty"`
}

// MungeKeySecret defines the [corev1.Secret] reference required for inter-server communication of Slurm nodes
type MungeKeySecret struct {
	// Name defines the name of the Slurm key secret
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key defines the key in the secret containing the Slurm key
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// SSHPublicKeysSecret defines the [corev1.Secret] reference required for SSH connection to Slurm login nodes
type SSHPublicKeysSecret struct {
	// Name defines the name of the Slurm key secret
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Keys defines the keys in the secret containing particular SSH public keys
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Keys []string `json:"keys"`
}

// SlurmNodes define the desired state of the Slurm nodes
type SlurmNodes struct {
	// Controller represents the Slurm controller node configuration
	//
	// +kubebuilder:validation:Required
	Controller SlurmNodeController `json:"controller"`

	// Worker represents the Slurm worker node configuration
	//
	// +kubebuilder:validation:Required
	Worker SlurmNodeWorker `json:"worker"`

	// Login represents the Slurm login node configuration
	//
	// +kubebuilder:validation:Required
	Login SlurmNodeLogin `json:"login"`
}

// SlurmNodeController defines the configuration for the Slurm controller node
type SlurmNodeController struct {
	SlurmNode `json:",inline"`

	// Slurmctld represents the Slurm control daemon configuration
	//
	// +kubebuilder:validation:Required
	Slurmctld NodeContainer `json:"slurmctld"`

	// Munge represents the Slurm munge configuration
	//
	// +kubebuilder:validation:Required
	Munge NodeContainer `json:"munge"`

	// Volumes represents the volume configurations for the controller node
	//
	// +kubebuilder:validation:Required
	Volumes SlurmNodeControllerVolumes `json:"volumes"`
}

// SlurmNodeControllerVolumes define the volumes for the Slurm controller node
type SlurmNodeControllerVolumes struct {
	// Spool represents the spool data volume configuration
	//
	// +kubebuilder:validation:Required
	Spool NodeVolume `json:"spool"`

	// Jail represents the jail data volume configuration
	//
	// +kubebuilder:validation:Required
	Jail NodeVolume `json:"jail"`
}

// SlurmNodeWorker defines the configuration for the Slurm worker node
type SlurmNodeWorker struct {
	SlurmNode `json:",inline"`

	// MaxGPU represents the maximal number of GPUs available for one worker
	//
	// +kubebuilder:validation:Required
	MaxGPU int32 `json:"maxGpu"`

	// Slurmd represents the Slurm daemon service configuration
	//
	// +kubebuilder:validation:Required
	Slurmd NodeContainer `json:"slurmd"`

	// Munge represents the Slurm munge configuration
	//
	// +kubebuilder:validation:Required
	Munge NodeContainer `json:"munge"`

	// Volumes represents the volume configurations for the worker node
	//
	// +kubebuilder:validation:Required
	Volumes SlurmNodeWorkerVolumes `json:"volumes"`
}

// SlurmNodeWorkerVolumes defines the volumes for the Slurm worker node
type SlurmNodeWorkerVolumes struct {
	// Spool represents the spool data volume configuration
	//
	// +kubebuilder:validation:Required
	Spool NodeVolume `json:"spool"`

	// Jail represents the jail data volume configuration
	//
	// +kubebuilder:validation:Required
	Jail NodeVolume `json:"jail"`

	// JailSubMounts represents the sub-mount configurations within the jail volume
	//
	// +kubebuilder:validation:Required
	JailSubMounts []NodeVolumeJailSubMount `json:"jailSubMounts"`
}

// SlurmNodeLogin defines the configuration for the Slurm login node
type SlurmNodeLogin struct {
	SlurmNode `json:",inline"`

	// Sshd represents the SSH daemon service configuration
	//
	// +kubebuilder:validation:Required
	Sshd NodeContainer `json:"sshd"`

	// SshdServiceType represents the service type for the SSH daemon
	//
	// +kubebuilder:validation:Required
	SshdServiceType corev1.ServiceType `json:"sshdServiceType"`

	// Volumes represents the volume configurations for the login node
	//
	// +kubebuilder:validation:Required
	Volumes SlurmNodeLoginVolumes `json:"volumes"`
}

// SlurmNodeLoginVolumes defines the volumes for the Slurm login node
type SlurmNodeLoginVolumes struct {
	// Users represents the user data volume configuration
	//
	// +kubebuilder:validation:Required
	Users NodeVolume `json:"users"`

	// Jail represents the jail data volume configuration
	//
	// +kubebuilder:validation:Required
	Jail NodeVolume `json:"jail"`

	// JailSubMounts represents the sub-mount configurations within the jail volume
	//
	// +kubebuilder:validation:Required
	JailSubMounts []NodeVolumeJailSubMount `json:"jailSubMounts"`
}

// SlurmNode represents the common configuration for a Slurm node.
type SlurmNode struct {
	// Size defines the number of node instances
	Size int32 `json:"size,omitempty"`

	// K8sNodeFilterName defines the Kubernetes node filter name associated with the Slurm node.
	// Must correspond to the name of one of [K8sNodeFilter]
	//
	// +kubebuilder:validation:Required
	K8sNodeFilterName string `json:"k8sNodeFilterName"`
}

// NodeContainer defines the configuration for one of node containers
type NodeContainer struct {
	// Image defines the container image
	//
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// Port defines the port the container exposes
	//
	// +kubebuilder:validation:Optional
	Port int32 `json:"port,omitempty"`

	// Resources defines the resource requirements for the container
	//
	// +kubebuilder:validation:Optional
	Resources NodeServiceResources `json:"resources,omitempty"`
}

// NodeServiceResources defines the resource requirements for a node service
type NodeServiceResources struct {
	// CPU defines the CPU resource requirement
	//
	// +kubebuilder:validation:Required
	CPU resource.Quantity `json:"cpu"`

	// Memory defines the memory resource requirement
	//
	// +kubebuilder:validation:Required
	Memory resource.Quantity `json:"memory"`

	// EphemeralStorage defines the ephemeral storage resource requirement
	//
	// +kubebuilder:validation:Required
	EphemeralStorage resource.Quantity `json:"ephemeralStorage"`
}

// NodeVolume defines the configuration for a node volume.
// Only one source type must be specified
type NodeVolume struct {
	// VolumeSourceName defines the name of the volume source.
	// Must correspond to the name of one of [VolumeSource]
	//
	// +kubebuilder:validation:Optional
	VolumeSourceName *string `json:"volumeSourceName,omitempty"`

	// VolumeClaimTemplateSpec defines the [corev1.PersistentVolumeClaim] template specification
	//
	// +kubebuilder:validation:Optional
	VolumeClaimTemplateSpec *corev1.PersistentVolumeClaimSpec `json:"volumeClaimTemplateSpec,omitempty"`
}

// NodeVolumeJailSubMount defines the configuration for a sub-mount within a jail volume
type NodeVolumeJailSubMount struct {
	// Name defines the name of the sub-mount
	//
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// MountPath defines the path where the sub-mount is mounted
	//
	// +kubebuilder:validation:Required
	MountPath string `json:"mountPath"`

	// VolumeSourceName defines the name of the volume source for the sub-mount.
	// Must correspond to the name of one of [VolumeSource]
	//
	// +kubebuilder:validation:Required
	VolumeSourceName string `json:"volumeSourceName"`
}

const (
	ConditionClusterControllersAvailable = "ControllersAvailable"
	ConditionClusterWorkersAvailable     = "WorkersAvailable"

	PhaseClusterReconciling  = "Reconciling"
	PhaseClusterNotAvailable = "Not available"
	PhaseClusterAvailable    = "Available"
)

// SlurmClusterStatus defines the observed state of SlurmCluster
type SlurmClusterStatus struct {
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +kubebuilder:validation:Optional
	Phase *string `json:"phase,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// SlurmCluster is the Schema for the slurmclusters API
//
// +kubebuilder:printcolumn:name="Controllers",type=integer,JSONPath=`.spec.slurmNodes.controller.size`,description="The number of controller nodes"
// +kubebuilder:printcolumn:name="Workers",type=integer,JSONPath=`.spec.slurmNodes.worker.size`,description="The number of worker nodes"
// +kubebuilder:printcolumn:name="Login",type=integer,JSONPath=`.spec.slurmNodes.login.size`,description="The number of login nodes"
type SlurmCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SlurmClusterSpec   `json:"spec,omitempty"`
	Status SlurmClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SlurmClusterList contains a list of SlurmCluster
type SlurmClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SlurmCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SlurmCluster{}, &SlurmClusterList{})
}
