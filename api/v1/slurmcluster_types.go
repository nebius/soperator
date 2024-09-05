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

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

// SlurmClusterSpec defines the desired state of SlurmCluster
type SlurmClusterSpec struct {
	// CRVersion defines the version of the Operator the Custom Resource belongs to
	//
	// +kubebuilder:validation:Optional
	CRVersion string `json:"crVersion,omitempty"` // TODO backward compatibility

	// Pause defines whether to gracefully stop the cluster.
	// Setting it to false after cluster has been paused starts the cluster back
	//
	// +kubebuilder:validation:Optional
	Pause bool `json:"pause,omitempty"` // TODO cluster pausing/resuming

	// NCCLSettings
	// +kubebuilder:validation:Optional
	NCCLSettings NCCLSettings `json:"ncclSettings,omitempty"`

	// PopulateJail defines the k8s Job that performs initial jail file system population
	//
	// +kubebuilder:validation:Required
	PopulateJail PopulateJail `json:"populateJail"`

	// PeriodicChecks define the k8s CronJobs performing cluster checks
	//
	// +kubebuilder:validation:Required
	PeriodicChecks PeriodicChecks `json:"periodicChecks"`

	// K8sNodeFilters define the k8s node filters used in Slurm node specifications
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	K8sNodeFilters []K8sNodeFilter `json:"k8sNodeFilters"`

	// VolumeSources define the sources for the volumes
	//
	// +kubebuilder:validation:Optional
	VolumeSources []VolumeSource `json:"volumeSources,omitempty"`

	// Secrets define the [corev1.Secret] references needed for Slurm cluster operation
	//
	// +kubebuilder:validation:Required
	Secrets Secrets `json:"secrets"`

	// SlurmNodes define the desired state of Slurm nodes
	//
	// +kubebuilder:validation:Required
	SlurmNodes SlurmNodes `json:"slurmNodes"`

	// Metrics define the desired state of the prometheus or opentelemetry metrics
	//
	// +kubebuilder:validation:Optional
	Telemetry *Telemetry `json:"telemetry,omitempty"`
}

type NCCLSettings struct {

	// TopologyType define type of NCCL GPU topology
	//
	// +kubebuilder:validation:Enum="H100 GPU cluster";auto;custom
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="auto"
	TopologyType string `json:"topologyType,omitempty"`

	// TopologyData defines NCCL GPU topology
	//
	// +kubebuilder:validation:Optional
	TopologyData string `json:"topologyData,omitempty"`
}

type PopulateJail struct {
	// Image defines the populate jail container image
	//
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// K8sNodeFilterName defines the Kubernetes node filter name associated with the Slurm node.
	// Must correspond to the name of one of [K8sNodeFilter]
	//
	// +kubebuilder:validation:Required
	K8sNodeFilterName string `json:"k8sNodeFilterName"`

	// JailSnapshotVolume represents configuration of the volume containing the initial content of the jail root
	// directory. If not set, the default content is used (the Image already includes this default content)
	//
	// +kubebuilder:validation:Optional
	JailSnapshotVolume *NodeVolume `json:"jailSnapshotVolume,omitempty"`

	// Overwrite defines whether to overwrite content on the jail volume if it's already populated.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	Overwrite bool `json:"overwrite"`
}

// PeriodicChecks define the k8s CronJobs performing cluster checks
type PeriodicChecks struct {
	// NCCLBenchmark defines the desired state of nccl benchmark
	//
	// +kubebuilder:validation:Required
	NCCLBenchmark NCCLBenchmark `json:"ncclBenchmark"`
}

// NCCLBenchmark defines the desired state of nccl benchmark
type NCCLBenchmark struct {
	// Enabled defines whether the CronJob should be scheduled
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Schedule defines the CronJob schedule.
	// By default, runs benchmark every 3 hours
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="0 */3 * * *"
	Schedule string `json:"schedule,omitempty"`

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
	// +kubebuilder:default=3
	FailedJobsHistoryLimit int32 `json:"failedJobsHistoryLimit,omitempty"`

	// Image defines the nccl container image
	//
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// NCCLArguments define nccl settings
	//
	// +kubebuilder:validation:Optional
	NCCLArguments NCCLArguments `json:"ncclArguments,omitempty"`

	// FailureActions define actions performed on benchmark failure
	//
	// +kubebuilder:validation:Optional
	FailureActions FailureActions `json:"failureActions,omitempty"`

	// K8sNodeFilterName defines the Kubernetes node filter name associated with the Slurm node.
	// Must correspond to the name of one of [K8sNodeFilter]
	//
	// +kubebuilder:validation:Required
	K8sNodeFilterName string `json:"k8sNodeFilterName"`
}

// NCCLArguments define nccl settings for periodic nccl benchmark
type NCCLArguments struct {
	// MinBytes defines the minimum memory size to start nccl with
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="512Mb"
	MinBytes string `json:"minBytes,omitempty"`

	// MaxBytes defines the maximum memory size to finish nccl with
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="8Gb"
	MaxBytes string `json:"maxBytes,omitempty"`

	// StepFactor defines the multiplication factor between two sequential memory sizes
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="2"
	StepFactor string `json:"stepFactor,omitempty"`

	// Timeout defines the timeout for nccl in its special format
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="20:00"
	Timeout string `json:"timeout,omitempty"`

	// ThresholdMoreThan defines the threshold for benchmark result that must be guaranteed.
	// CronJob will fail if the result is less than the threshold
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="0"
	ThresholdMoreThan string `json:"thresholdMoreThan,omitempty"`

	// UseInfiniband defines using NCCL_P2P_DISABLE=1 NCCL_SHM_DISABLE=1 NCCL_ALGO=Ring env variables for test
	// https://docs.nvidia.com/deeplearning/nccl/user-guide/docs/env.html
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	UseInfiniband bool `json:"useInfiniband,omitempty"`
}

// FailureActions define actions performed on benchmark failure
type FailureActions struct {
	// SetSlurmNodeDrainState defines whether to drain Slurm node in case of benchmark failure
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	SetSlurmNodeDrainState bool `json:"setSlurmNodeDrainState,omitempty"`
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

// Secrets define the [corev1.Secret] references needed for Slurm cluster operation
type Secrets struct {
	// SshdKeysName defines name of the [corev1.Secret] with ssh keys for SSHD server
	// +kubebuilder:validation:Optional
	SshdKeysName string `json:"sshdKeysName,omitempty"`
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
	// CgroupVersion defines the version of the cgroup
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="v2"
	// +kubebuilder:validation:Enum="v1";"v2"
	CgroupVersion string `json:"cgroupVersion,omitempty"`
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

	// Size of the shared memory for NCCL
	//
	// +kubebuilder:default="64Gi"
	SharedMemorySize *resource.Quantity `json:"sharedMemorySize,omitempty"`
}

// SlurmNodeLogin defines the configuration for the Slurm login node
type SlurmNodeLogin struct {
	SlurmNode `json:",inline"`

	// Sshd represents the SSH daemon service configuration
	//
	// +kubebuilder:validation:Required
	Sshd NodeContainer `json:"sshd"`

	// Munge represents the Slurm munge configuration
	//
	// +kubebuilder:validation:Required
	Munge NodeContainer `json:"munge"`

	// SshdServiceType represents the service type for the SSH daemon
	// Must be one of [corev1.ServiceTypeLoadBalancer] or [corev1.ServiceTypeNodePort]
	//
	// +kubebuilder:validation:Required
	SshdServiceType corev1.ServiceType `json:"sshdServiceType"`

	// SshdServiceAnnotations represent K8S annotations that should be added to the Login node service
	//
	// +kubebuilder:validation:Optional
	SshdServiceAnnotations map[string]string `json:"sshdServiceAnnotations,omitempty"`

	// SshRootPublicKeys represents the list of public authorized_keys for SSH connection to Slurm login nodes
	//
	// +kubebuilder:validation:Required
	SshRootPublicKeys []string `json:"sshRootPublicKeys"`

	// SshdServiceLoadBalancerIP represents the static IP address of the LoadBalancer service
	//
	// +kubebuilder:validation:Optional
	SshdServiceLoadBalancerIP string `json:"sshdServiceLoadBalancerIP,omitempty"`

	// SshdServiceNodePort represents the port to be opened on nodes in case of NodePort type of service
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=0
	SshdServiceNodePort int32 `json:"sshdServiceNodePort,omitempty"`

	// Volumes represents the volume configurations for the login node
	//
	// +kubebuilder:validation:Required
	Volumes SlurmNodeLoginVolumes `json:"volumes"`
}

// SlurmNodeLoginVolumes defines the volumes for the Slurm login node
type SlurmNodeLoginVolumes struct {
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

	// Resources defines the [corev1.ResourceRequirements] for the container
	//
	// +kubebuilder:validation:Optional
	Resources corev1.ResourceList `json:"resources,omitempty"`
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

type Telemetry struct {
	// It has to be set to true if OpenTelemetry Operator CRD is used
	//
	// +kubebuilder:validation:Optional
	OpenTelemetryCollector *MetricsOpenTelemetryCollector `json:"openTelemetryCollector,omitempty"`

	// It has to be set to true if Prometheus Operator CRD is used
	//
	// +kubebuilder:validation:Optional
	Prometheus *MetricsPrometheus `json:"prometheus,omitempty"`

	// It has to be set to true if Kubernetes events for Slurm jobs are sent
	//
	// +kubebuilder:validation:Optional
	JobsTelemetry *JobsTelemetry `json:"jobsTelemetry,omitempty"`
}

type MetricsOpenTelemetryCollector struct {
	// It has to be set to true if OpenTelemetry Operator is used
	//
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// It references the PodTemplate with the OpenTelemetry Collector configuration
	//
	// +kubebuilder:validation:Optional
	PodTemplateNameRef *string `json:"podTemplateNameRef,omitempty"`

	// It defines the number of replicas for the OpenTelemetry Collector
	//
	// +kubebuilder:default=1
	ReplicasOtelCollector int32 `json:"replicasOtelCollector,omitempty"`
	// Specifies the port for OtelCollector
	//
	// +kubebuilder:default=4317
	OtelCollectorPort int32 `json:"otelCollectorPort,omitempty"`
}

type MetricsPrometheus struct {
	// It has to be set to true if Prometheus Operator is used
	//
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`
	// Specifies image for Slurm Exporter
	//
	// +kubebuilder:validation:Optional
	ImageSlurmExporter *string `json:"imageSlurmExporter,omitempty"`
	// It references the PodTemplate with the Slurm Exporter configuration
	//
	// +kubebuilder:validation:Optional
	PodTemplateNameRef *string `json:"podTemplateNameRef,omitempty"`
	// Resources defines the [corev1.ResourceRequirements] for the container the Slurm Exporter
	//
	// +kubebuilder:validation:Optional
	ResourcesSlurmExporter corev1.ResourceList `json:"resources,omitempty"`
	// It references the PodMonitor configuration
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default={jobLabel: "slurm-exporter", interval: "30s", scrapeTimeout: "20s"}
	PodMonitorConfig PodMonitorConfig `json:"podMonitorConfig,omitempty"`
}

type JobsTelemetry struct {
	// Defines whether to send Kubernetes events for Slurm NCCLBenchmark jobs
	//
	// +kubebuilder:default=false
	SendJobsEvents bool `json:"sendJobsEvents,omitempty"`

	// Defines whether to send Opentelemetry metrics for Slurm NCCLBenchmark jobs
	//
	// +kubebuilder:default=false
	SendOtelMetrics bool `json:"sendOtelMetrics,omitempty"`

	// Specifies the gRPC OtelCollector host for sending Opentelemetry metrics
	//
	// +kubebuilder:validation:Optional
	OtelCollectorGrpcHost *string `json:"otelCollectorGrpcHost,omitempty"`

	// Specifies the HTTP OtelCollector host for sending Opentelemetry metrics
	//
	// +kubebuilder:validation:Optional
	OtelCollectorHttpHost *string `json:"otelCollectorHttpHost,omitempty"`

	// Specifies the port for OtelCollector
	//
	// +kubebuilder:default=4317
	OtelCollectorPort int32 `json:"otelCollectorPort,omitempty"`
	// Specifies the path to the OtelCollector endpoint for sending Opentelemetry metrics
	//
	// +kubebuilder:default="/v1/metrics"
	OtelCollectorPath string `json:"otelCollectorPath,omitempty"`
}

// PodMonitor defines a prometheus PodMonitor object.
type PodMonitorConfig struct {
	// JobLabel to add to the PodMonitor object. If not set, the default value is "slurm-exporter"
	//
	// +kubebuilder:validation:Optional +kubebuilder:default="slurm-exporter"
	JobLabel string `json:"jobLabel,omitempty"`
	// Interval for scraping metrics. 30s by default.
	//
	// +kubebuilder:validation:Optional
	Interval prometheusv1.Duration `json:"interval,omitempty"`
	// ScrapeTimeout defines the timeout for scraping metrics.
	//
	// +kubebuilder:validation:Optional
	ScrapeTimeout prometheusv1.Duration `json:"scrapeTimeout,omitempty"`
	// RelabelConfig allows dynamic rewriting of the label set for targets, alerts,
	//
	// +kubebuilder:validation:Optional
	RelabelConfig []prometheusv1.RelabelConfig `json:"relabelConfig,omitempty"`
	// `metricRelabelings` configures the relabeling rules to apply to the samples before ingestion.
	//
	// +kubebuilder:validation:Optional
	MetricRelabelConfigs []prometheusv1.RelabelConfig `json:"metricRelabelConfigs,omitempty"`
}

const (
	SlurmClusterKind = "SlurmCluster"

	ConditionClusterCommonAvailable      = "CommonAvailable"
	ConditionClusterControllersAvailable = "ControllersAvailable"
	ConditionClusterWorkersAvailable     = "WorkersAvailable"
	ConditionClusterLoginAvailable       = "LoginAvailable"

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
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.phase`,description="The phase of Slurm cluster creation."
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
