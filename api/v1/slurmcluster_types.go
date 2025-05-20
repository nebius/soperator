package v1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

// SlurmClusterSpec defines the desired state of SlurmCluster
type SlurmClusterSpec struct {
	// CRVersion defines the version of the Operator the Custom Resource belongs to
	//
	// +kubebuilder:validation:Optional
	CRVersion string `json:"crVersion,omitempty"` // TODO backward compatibility

	// ClusterType define type of slurm worker nodes
	// +kubebuilder:validation:Enum=gpu;cpu
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="gpu"
	ClusterType string `json:"clusterType,omitempty"`
	// Maintenance defines the maintenance window for the cluster.
	// It can have the following values:
	// - none: No maintenance is performed. The cluster operates normally.
	// - downscale: Scales down all components to 0.
	// - downscaleAndDeletePopulateJail: Scales down all components to 0 and deletes the kubernetes Kind Jobs populateJail.
	// - downscaleAndOverwritePopulateJail: Scales down all components to 0 and overwrite populateJail (same as overwrite=true).
	// - skipPopulateJail: Skips the execution of the populateJail job during maintenance.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=none;downscale;downscaleAndDeletePopulateJail;downscaleAndOverwritePopulateJail;skipPopulateJail
	// +kubebuilder:default="none"
	Maintenance *consts.MaintenanceMode `json:"maintenance,omitempty"`

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

	// PartitionConfiguration define partition configuration of slurm worker nodes
	// https://slurm.schedmd.com/slurm.conf.html#SECTION_PARTITION-CONFIGURATION
	// +kubebuilder:validation:Optional
	PartitionConfiguration PartitionConfiguration `json:"partitionConfiguration,omitempty"`

	// SlurmConfig represents the Slurm configuration in slurm.conf. Not all options are supported.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default={defMemPerNode: 1228800, defCpuPerGPU: 16, completeWait: 5, debugFlags: "Cgroup,CPU_Bind,Gres,JobComp,Priority,Script,SelectType,Steps,TraceJobs", epilog: "", prolog: "", taskPluginParam: "", maxJobCount: 10000, minJobAge: 86400}
	SlurmConfig SlurmConfig `json:"slurmConfig,omitempty"`

	// CustomSlurmConfig represents the raw Slurm configuration from slurm.conf.
	// All options are provided as a raw string.
	// Soperator does not guarantee the validity of the raw configuration.
	// Raw config is merged with existing SlurmConfig values.
	//
	// +kubebuilder:validation:Optional
	CustomSlurmConfig *string `json:"customSlurmConfig,omitempty"`
	// MPIConfig represents the PMIx configuration in mpi.conf. Not all options are supported.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default={pmixEnv: "OMPI_MCA_btl_tcp_if_include=eth0"}
	MPIConfig MPIConfig `json:"mpiConfig,omitempty"`

	// SlurmTopologyConfigMapRefName is the name of the slurm topology config.
	// When exists, TopologyPlugin is automatically set to `topology/tree` in slurm.conf
	// if TopologyPlugin is not explicitly specified in SlurmConfig.
	//
	// +kubebuilder:validation:Optional
	SlurmTopologyConfigMapRefName string `json:"slurmTopologyConfigMapRefName,omitempty"`

	// SConfigController defines the desired state of controller that watches after configs
	//
	// +kubebuilder:validation:Optional
	SConfigController SConfigController `json:"sConfigController,omitempty"`

	// Generate and set default AppArmor profile for the Slurm worker and login nodes. The Security Profiles Operator must be installed.
	//
	// +kubebuilder:default=false
	UseDefaultAppArmorProfile bool `json:"useDefaultAppArmorProfile,omitempty"`

	// WorkerFeatures defines Slurm node features to be used in the workers and Slurm nodesets to create using these features.
	//
	// +kubebuilder:validation:Optional
	WorkerFeatures []WorkerFeature `json:"workerFeatures,omitempty"`

	// HealthCheckConfig defines Slurm health check configuration.
	//
	// +kubebuilder:validation:Optional
	HealthCheckConfig *HealthCheckConfig `json:"healthCheckConfig,omitempty"`
}

// SlurmConfig represents the Slurm configuration in slurm.conf
type SlurmConfig struct {
	// Default real memory size available per allocated node in mebibytes.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=1228800
	DefMemPerNode *int32 `json:"defMemPerNode,omitempty"`
	// Default count of CPUs allocated per allocated GPU
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=16
	DefCpuPerGPU *int32 `json:"defCpuPerGPU,omitempty"`
	// The time to wait, in seconds, when any job is in the COMPLETING state before any additional jobs are scheduled.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=5
	CompleteWait *int32 `json:"completeWait,omitempty"`
	// Defines specific subsystems which should provide more detailed event logging.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="Priority,Script,SelectType,Steps"
	// +kubebuilder:validation:Pattern="^((Accrue|Agent|AuditRPCs|Backfill|BackfillMap|BurstBuffer|Cgroup|ConMgr|CPU_Bind|CpuFrequency|Data|DBD_Agent|Dependency|Elasticsearch|Energy|Federation|FrontEnd|Gres|Hetjob|Gang|GLOB_SILENCE|JobAccountGather|JobComp|JobContainer|License|Network|NetworkRaw|NodeFeatures|NO_CONF_HASH|Power|Priority|Profile|Protocol|Reservation|Route|Script|SelectType|Steps|Switch|TLS|TraceJobs|Triggers)(,)?)+$"
	DebugFlags *string `json:"debugFlags,omitempty"`
	// Defines specific file to run the epilog when job ends. Default value is no epilog
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=""
	Epilog *string `json:"epilog,omitempty"`
	// Defines specific file to run the prolog when job starts. Default value is no prolog
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=""
	Prolog *string `json:"prolog,omitempty"`
	// Additional parameters for the task plugin
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=""
	// +kubebuilder:validation:Pattern="^(|((None|Cores|Sockets|Threads|SlurmdOffSpec|OOMKillStep|Verbose|Autobind)(,)?)+)$"
	TaskPluginParam *string `json:"taskPluginParam,omitempty"`
	// Keep N last jobs in controller memory
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=10000
	MaxJobCount *int32 `json:"maxJobCount,omitempty"`
	// Don't remove jobs from controller memory after some time
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=86400
	MinJobAge *int32 `json:"minJobAge,omitempty"`
	// MessageTimeout specifies the permitted time for a round-trip communication to complete in seconds.
	// See https://slurm.schedmd.com/slurm.conf.html#OPT_MessageTimeout.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=30
	MessageTimeout *int32 `json:"messageTimeout,omitempty"`
	// TopologyPlugin identifies the plugin to determine network topology for optimizations.
	// It is set automatically to `topology/tree` if SlurmTopologyConfigMapRefName is specified.
	//
	// +kubebuilder:validation:Optional
	TopologyPlugin string `json:"topologyPlugin,omitempty"`
	// TopologyParam is list of comma-separated options identifying network topology options.
	//
	// +kubebuilder:validation:Optional
	TopologyParam string `json:"topologyParam,omitempty"`
}

type MPIConfig struct {
	// Semicolon separated list of environment variables to be set in job environments to be used by PMIx.
	// Defaults to "OMPI_MCA_btl_tcp_if_include=eth0" to avoid "lo" and "docker" interfaces to be selected by OpenMPI.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="OMPI_MCA_btl_tcp_if_include=eth0"
	// +kubebuilder:validation:Optional
	PMIxEnv string `json:"pmixEnv,omitempty"`
}

type SConfigController struct {
	Node      SlurmNode     `json:"node,omitempty"`
	Container NodeContainer `json:"container,omitempty"`
}

type PartitionConfiguration struct {
	// ConfigType
	// +kubebuilder:validation:Enum=default;custom
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="default"
	ConfigType string `json:"configType,omitempty"`
	// RawConfig define partition configuration as list of string started with PartitionName
	// Example for custom ConfigType:
	// - PartitionName=low_priority Nodes=worker-[0-15] Default=YES MaxTime=INFINITE State=UP PriorityTier=1
	// - PartitionName=high_priority  Nodes=worker-[10-20] Default=NO MaxTime=INFINITE State=UP PriorityTier=2
	// +kubebuilder:validation:Optional
	RawConfig []string `json:"rawConfig,omitempty"`
}

type WorkerFeature struct {
	// Name defines the name of the feature.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// HostlistExpr defines a Slurm hostlist expression, e.g. "workers-[0-2,10],workers-[3-5]".
	// Soperator will run these workers with the feature name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	HostlistExpr string `json:"hostlistExpr,omitempty"`
	// NodesetName optionally defines the Slurm nodeset name to be provisioned using this feature.
	// This nodeset maybe be used in conjunction with partitions.
	// +kubebuilder:validation:Optional
	NodesetName string `json:"nodesetName,omitempty"`
}

type HealthCheckConfig struct {
	// HealthCheckInterval defines interval for health check run in seconds.
	//
	// +kubebuilder:validation:Required
	HealthCheckInterval int32 `json:"healthCheckInterval"`

	// HealthCheckProgram defines program for health check run.
	//
	// +kubebuilder:validation:Required
	HealthCheckProgram string `json:"healthCheckProgram"`

	// HealthCheckNodeState identifies what node states should execute the HealthCheckProgram.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	HealthCheckNodeState []HealthCheckNodeState `json:"healthCheckNodeState"`
}

type HealthCheckNodeState struct {
	// State identifies node state on which HealthCheckProgram should be executed.
	//
	// +kubebuilder:validation:Enum=ALLOC;ANY;CYCLE;IDLE;NONDRAINED_IDLE;MIXED
	// +kubebuilder:validation:Required
	State string `json:"state"`
}

type NCCLSettings struct {

	// TopologyType define type of NCCL GPU topology
	//
	// +kubebuilder:validation:Enum=auto;custom
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

	// ImagePullPolicy defines the image pull policy
	//
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="IfNotPresent"
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

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

	// AppArmorProfile defines the AppArmor profile for the Slurm node
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="unconfined"
	AppArmorProfile string `json:"appArmorProfile,omitempty"`
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
	// +kubebuilder:default=16
	FailedJobsHistoryLimit int32 `json:"failedJobsHistoryLimit,omitempty"`

	// Image defines the nccl container image
	//
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// ImagePullPolicy defines the image pull policy
	//
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="IfNotPresent"
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

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

	// AppArmorProfile defines the AppArmor profile for the Slurm node
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="unconfined"
	AppArmorProfile string `json:"appArmorProfile,omitempty"`
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

	// UseInfiniband defines using NCCL_P2P_DISABLE=1 NCCL_SHM_DISABLE=1 NCCL_ALGO=Ring env variables for test.
	// According to NVIDIA these env vars should be used only for debugging.
	// https://docs.nvidia.com/deeplearning/nccl/user-guide/docs/env.html
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=true
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
	// Accounting represents the Slurm accounting node and database configuration
	//
	// TODO: Making accounting optional requires SlurmNode.K8sNodeFilterName to be optional.
	// +kubebuilder:validation:Required
	Accounting SlurmNodeAccounting `json:"accounting"`

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

	// Exporter represents the Slurm exporter node configuration
	//
	// TODO: Making exporter optional requires SlurmNode.K8sNodeFilterName to be optional.
	// +kubebuilder:validation:Required
	Exporter SlurmExporter `json:"exporter"`

	// Rest represents the Slurm REST API node configuration
	//
	// +kubebuilder:validation:Required
	Rest SlurmRest `json:"rest"`
}

// SlurmRest represents the Slur REST API configuration
type SlurmRest struct {
	SlurmNode `json:",inline"`

	// Enabled defines whether the SlurmRest is enabled
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// SlurmRestNode represents the Slurm REST API daemon configuration
	//
	// +kubebuilder:validation:Optional
	SlurmRestNode NodeContainer `json:"rest,omitempty"`
}

// SlurmNodeAccounting represents the Slurm accounting configuration
type SlurmNodeAccounting struct {
	SlurmNode `json:",inline"`

	// Enabled defines whether the SlurmDBD is enabled
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Slurmdbd represents the Slurm database daemon configuration
	//
	// +kubebuilder:validation:Optional
	Slurmdbd NodeContainer `json:"slurmdbd,omitempty"`

	// Munge represents the Slurm munge configuration
	//
	// +kubebuilder:validation:Optional
	Munge NodeContainer `json:"munge,omitempty"`

	// ExternalDB represents the external database configuration of connection string
	//
	// +kubebuilder:validation:Optional
	ExternalDB ExternalDB `json:"externalDB,omitempty"`
	// MariaDbOpeator represents the MariaDB CRD configuration
	//
	// +kubebuilder:validation:Optional
	MariaDbOperator MariaDbOperator `json:"mariadbOperator,omitempty"`
	// SlurmdbdConfig represents some the SlurmDBD.conf configuration
	//
	// +kubebuilder:validation:Optional
	SlurmdbdConfig SlurmdbdConfig `json:"slurmdbdConfig,omitempty"`

	// SlurmConfig represents the Slurm accounting configuration in slurm.conf
	//
	// +kubebuilder:validation:Optional
	SlurmConfig AccountingSlurmConf `json:"slurmConfig,omitempty"`
}

// ExternalDB represents the external database configuration of connection string
type ExternalDB struct {
	// Enabled defines whether the external database is enabled
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// Host for connection string to the SlurmDBD database
	//
	// +kubebuilder:validation:Optional
	Host string `json:"host"`
	// Port for connection string to the SlurmDBD database
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=3306
	Port int32 `json:"port"`
	// Key defines the key of username and password in the secret
	//
	// +kubebuilder:validation:Optional
	User string `json:"user"`
	// SecretRef defines the reference to the secret with the password key for the external database
	//
	// +kubebuilder:validation:Optional
	PasswordSecretKeyRef PasswordSecretKeyRef `json:"passwordSecretKeyRef"`
}

type PasswordSecretKeyRef struct {
	// Name defines the name of the secret
	//
	// +kubebuilder:validation:Optional
	Name string `json:"name"`
	// Key defines the key of password in the secret (do not put here the password, just name of the key in the secret)
	//
	// +kubebuilder:validation:Optional
	Key string `json:"key"`
}

type MariaDbOperator struct {
	// +kubebuilder:validation:Optional
	Enabled bool `json:"enabled"`

	// If enabled, secret cannot be deleted until custom resource slurmcluster is deleted
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	// +kubebuilder:validation:Immutable
	ProtectedSecret bool `json:"protectedSecret"`

	NodeContainer      `json:",inline"`
	PodSecurityContext *mariadbv1alpha1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	SecurityContext    *mariadbv1alpha1.SecurityContext    `json:"securityContext,omitempty"`
	Replicas           int32                               `json:"replicas,omitempty"`
	Metrics            MariadbMetrics                      `json:"metrics,omitempty"`
	Replication        *mariadbv1alpha1.Replication        `json:"replication,omitempty"`
	Storage            mariadbv1alpha1.Storage             `json:"storage,omitempty"`
}

type MariadbMetrics struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`
}

type SlurmdbdConfig struct {
	// +kubebuilder:validation:Enum="yes";"no"
	// +kubebuilder:default="yes"
	ArchiveEvents string `json:"archiveEvents,omitempty"`
	// +kubebuilder:validation:Enum="yes";"no"
	// +kubebuilder:default="yes"
	ArchiveJobs string `json:"archiveJobs,omitempty"`
	// +kubebuilder:validation:Enum="yes";"no"
	// +kubebuilder:default="yes"
	ArchiveResvs string `json:"archiveResvs,omitempty"`
	// +kubebuilder:validation:Enum="yes";"no"
	// +kubebuilder:default="no"
	ArchiveSteps string `json:"archiveSteps,omitempty"`
	// +kubebuilder:validation:Enum="yes";"no"
	// +kubebuilder:default="no"
	ArchiveSuspend string `json:"archiveSuspend,omitempty"`
	// +kubebuilder:validation:Enum="yes";"no"
	// +kubebuilder:default="no"
	ArchiveTXN string `json:"archiveTXN,omitempty"`
	// +kubebuilder:validation:Enum="yes";"no"
	// +kubebuilder:default="yes"
	ArchiveUsage string `json:"archiveUsage,omitempty"`
	// +kubebuilder:validation:Enum="quiet";"fatal";"error";"info";"verbose";"debug";"debug2";"debug3";"debug4";"debug5"
	// +kubebuilder:default="info"
	DebugLevel string `json:"debugLevel,omitempty"`
	// +kubebuilder:default=2
	TCPTimeout int16 `json:"tcpTimeout,omitempty"`
	// +kubebuilder:default="1month"
	PurgeEventAfter string `json:"purgeEventAfter,omitempty"`
	// +kubebuilder:default="12month"
	PurgeJobAfter string `json:"purgeJobAfter,omitempty"`
	// +kubebuilder:default="1month"
	PurgeResvAfter string `json:"purgeResvAfter,omitempty"`
	// +kubebuilder:default="1month"
	PurgeStepAfter string `json:"purgeStepAfter,omitempty"`
	// +kubebuilder:default="1month"
	PurgeSuspendAfter string `json:"purgeSuspendAfter,omitempty"`
	// +kubebuilder:default="12month"
	PurgeTXNAfter string `json:"purgeTXNAfter,omitempty"`
	// +kubebuilder:default="24month"
	PurgeUsageAfter string `json:"purgeUsageAfter,omitempty"`
	// +kubebuilder:validation:Optional
	PrivateData string `json:"privateData,omitempty"`
	// +kubebuilder:validation:Enum="AuditRPCs";"DB_ARCHIVE";"DB_ASSOC";"DB_EVENT";"DB_JOB";"DB_QOS";"DB_QUERY";"DB_RESERVATION";"DB_RESOURCE";"DB_STEP";"DB_TRES";"DB_USAGE";"Network"
	// +kubebuilder:validation
	DebugFlags string `json:"debugFlags,omitempty"`
}

type AccountingSlurmConf struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern="^((Billing|CPU|Mem|VMem|Node|Energy|Pages|FS/Disk|FS/Lustre|Gres/gpu)(,)?)+$"
	// +kubebuilder:default="CPU,Mem,Node,VMem,Gres/gpu"
	AccountingStorageTRES *string `json:"accountingStorageTRES,omitempty"`
	// +kubebuilder:validation:Optional
	AccountingStoreFlags *string `json:"accountingStoreFlags,omitempty"`
	// +kubebuilder:validation:Optional
	AcctGatherInterconnectType *string `json:"acctGatherInterconnectType,omitempty"`
	// +kubebuilder:validation:Optional
	AcctGatherFilesystemType *string `json:"acctGatherFilesystemType,omitempty"`
	// +kubebuilder:validation:Optional
	AcctGatherProfileType *string `json:"acctGatherProfileType,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum="jobacct_gather/linux";"jobacct_gather/cgroup";"jobacct_gather/none"
	// +kubebuilder:default="jobacct_gather/cgroup"
	JobAcctGatherType *string `json:"jobAcctGatherType,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=30
	JobAcctGatherFrequency *int `json:"jobAcctGatherFrequency,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum="NoShared";"UsePss";"OverMemoryKill";"DisableGPUAcct"
	JobAcctGatherParams *string `json:"jobAcctGatherParams,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=0
	PriorityWeightAge *int16 `json:"priorityWeightAge,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=0
	PriorityWeightFairshare *int16 `json:"priorityWeightFairshare,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=0
	PriorityWeightQOS *int16 `json:"priorityWeightQOS,omitempty"`
	// +kubebuilder:validation:Optional
	PriorityWeightTRES *string `json:"priorityWeightTRES,omitempty"`
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

	// CustomMounts represents the custom mount configurations
	//
	// +kubebuilder:validation:Optional
	CustomMounts []NodeVolumeMount `json:"customMounts,omitempty"`
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

	// SupervisordConfigMapRefName is the name of the supervisord config, which runs in slurmd container
	//
	// +kubebuilder:validation:Optional
	SupervisordConfigMapRefName string `json:"supervisordConfigMapRefName,omitempty"`

	// SSHDConfigMapRefName is the name of the SSHD config, which runs in slurmd container
	//
	// +kubebuilder:validation:Optional
	SSHDConfigMapRefName string `json:"sshdConfigMapRefName,omitempty"`

	// WorkerAnnotations represent K8S annotations that should be added to the workers
	//
	// +kubebuilder:validation:Optional
	WorkerAnnotations map[string]string `json:"workerAnnotations,omitempty"`

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

	// EnableGDRCopy driver propagation into containers (this feature must also be enabled in NVIDIA GPU operator)
	// https://developer.nvidia.com/gdrcopy
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	EnableGDRCopy bool `json:"enableGDRCopy,omitempty"`

	// SlurmNodeExtra defines the string that will be set to the "Extra" field of the corresponding Slurm node. It can
	// use any environment variables that are available in the slurmd container when it starts.
	//
	// +kubebuilder:validation:Optional
	SlurmNodeExtra string `json:"slurmNodeExtra,omitempty"`

	// PriorityClass defines the priority class for the Slurm worker node
	//
	// +kubebuilder:validation:Optional
	PriorityClass string `json:"priorityClass,omitempty"`
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
	JailSubMounts []NodeVolumeMount `json:"jailSubMounts"`

	// CustomMounts represents the custom mount configurations
	//
	// +kubebuilder:validation:Optional
	CustomMounts []NodeVolumeMount `json:"customMounts,omitempty"`

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

	// SSHDConfigMapRefName is the name of the SSHD config, which runs in login container
	//
	// +kubebuilder:validation:Optional
	SSHDConfigMapRefName string `json:"sshdConfigMapRefName,omitempty"`

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
	JailSubMounts []NodeVolumeMount `json:"jailSubMounts"`

	// CustomMounts represents the custom mount configurations
	//
	// +kubebuilder:validation:Optional
	CustomMounts []NodeVolumeMount `json:"customMounts,omitempty"`
}

// SlurmExporter defines the configuration for the Slurm exporter
type SlurmExporter struct {
	SlurmNode `json:",inline"`
	// It has to be set to true if Prometheus Operator is used
	//
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`
	// It references the PodMonitor configuration
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default={jobLabel: "slurm-exporter", interval: "30s", scrapeTimeout: "20s"}
	PodMonitorConfig PodMonitorConfig `json:"podMonitorConfig,omitempty"`
	// Exporter represents the Slurm exporter daemon configuration
	//
	// +kubebuilder:validation:Required
	Exporter ExporterContainer `json:"exporter,omitempty"`

	// Munge represents the Slurm munge configuration
	//
	// +kubebuilder:validation:Required
	// TODO: Optional?
	Munge NodeContainer `json:"munge"`

	// UseSoperatorExporter defines whether build-in cluster exporter is used
	// instead of third-party prometheus-slurm-exporter.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	UseSoperatorExporter bool `json:"useSoperatorExporter,omitempty"`

	// Volumes represents the volume configurations for the controller node
	//
	// +kubebuilder:validation:Required
	Volumes SlurmExporterVolumes `json:"volumes"`
}

// ExporterContainer defines the configuration for one of node containers
type ExporterContainer struct {
	NodeContainer `json:",inline"`

	// It references the PodTemplate with the Slurm Exporter configuration
	//
	// +kubebuilder:validation:Optional
	PodTemplateNameRef *string `json:"podTemplateNameRef,omitempty"`
}

// SlurmExporterVolumes define the volumes for the Slurm exporter node
type SlurmExporterVolumes struct {
	// Jail represents the jail data volume configuration
	//
	// +kubebuilder:validation:Required
	Jail NodeVolume `json:"jail"`
}

// SlurmNode represents the common configuration for a Slurm node.
type SlurmNode struct {
	// Size defines the number of node instances
	Size int32 `json:"size,omitempty"`

	// CustomInitContainers represent additional init containers that should be added to created Pods
	CustomInitContainers []corev1.Container `json:"customInitContainers,omitempty"`

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

	// Command defines the entrypoint array for the container. Not executed within a shell.
	// The container image's ENTRYPOINT is used if this is not provided.
	//
	// +kubebuilder:validation:Optional
	Command []string `json:"command,omitempty"`

	// Args defines the arguments to the entrypoint (command).
	// The container image's CMD is used if this is not provided.
	//
	// +kubebuilder:validation:Optional
	Args []string `json:"args,omitempty"`

	// ImagePullPolicy defines the image pull policy
	//
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="IfNotPresent"
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// Port defines the port the container exposes
	//
	// +kubebuilder:validation:Optional
	Port int32 `json:"port,omitempty"`

	// Resources defines the [corev1.ResourceRequirements] for the container
	//
	// +kubebuilder:validation:Optional
	Resources corev1.ResourceList `json:"resources,omitempty"`

	// SecurityLimitsConfig represents multiline limits.conf
	// format of a string should be: '* <soft|hard> <item> <value>'
	// example: '* soft nofile 1024'
	//
	// +kubebuilder:validation:Optional
	SecurityLimitsConfig string `json:"securityLimitsConfig,omitempty"`

	// AppArmorProfile defines the AppArmor profile for the Slurm containers
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="unconfined"
	AppArmorProfile string `json:"appArmorProfile,omitempty"`
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

// NodeVolumeMount defines the configuration for a mount
type NodeVolumeMount struct {
	// Name defines the name of the mount
	//
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// MountPath defines the path where the mount is mounted
	//
	// +kubebuilder:validation:Required
	MountPath string `json:"mountPath"`

	// SubPath points to a specific entry inside the volume.
	// Corresponds to the subPath field in the K8s volumeMount structure.
	// See official docs for details: https://kubernetes.io/docs/concepts/storage/volumes/#using-subpath
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=""
	SubPath string `json:"subPath"`

	// ReadOnly defines whether the mount point should be read-only
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	ReadOnly bool `json:"readOnly"`

	// VolumeSourceName defines the name of the volume source for the mount.
	// Must correspond to the name of one of [VolumeSource]
	//
	// +kubebuilder:validation:Optional
	VolumeSourceName *string `json:"volumeSourceName"`

	// VolumeClaimTemplateSpec defines the [corev1.PersistentVolumeClaim] template specification
	//
	// +kubebuilder:validation:Optional
	VolumeClaimTemplateSpec *corev1.PersistentVolumeClaimSpec `json:"volumeClaimTemplateSpec,omitempty"`
}

type Telemetry struct {
	// It has to be set to true if OpenTelemetry Operator CRD is used
	//
	// +kubebuilder:validation:Optional
	OpenTelemetryCollector *MetricsOpenTelemetryCollector `json:"openTelemetryCollector,omitempty"`
	//It has to be set to true if Kubernetes events for Slurm jobs are sent
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

// JobsTelemetry
// XValidation: both OtelCollectorGrpcHost and OtelCollectorHttpHost cannot have values at the same time
//
// +kubebuilder:validation:XValidation:rule="!(has(self.otelCollectorGrpcHost) && has(self.otelCollectorHttpHost))",message="Both OtelCollectorGrpcHost and OtelCollectorHttpHost cannot be set at the same time."
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

// PodMonitorConfig defines a prometheus PodMonitor object.
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
	KindSlurmCluster = "SlurmCluster"

	ConditionClusterCommonAvailable      = "CommonAvailable"
	ConditionClusterControllersAvailable = "ControllersAvailable"
	ConditionClusterWorkersAvailable     = "WorkersAvailable"
	ConditionClusterLoginAvailable       = "LoginAvailable"
	ConditionClusterAccountingAvailable  = "AccountingAvailable"
	ConditionClusterPopulateJailMode     = "PopulateJailMode"

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

func (s *SlurmClusterStatus) SetCondition(condition metav1.Condition) {
	if s.Conditions == nil {
		s.Conditions = make([]metav1.Condition, 0)
	}
	meta.SetStatusCondition(&s.Conditions, condition)
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// SlurmCluster is the Schema for the slurmclusters API
//
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.phase`,description="The phase of Slurm cluster creation."
// +kubebuilder:printcolumn:name="Controllers",type=integer,JSONPath=`.spec.slurmNodes.controller.size`,description="The number of controller nodes"
// +kubebuilder:printcolumn:name="Workers",type=integer,JSONPath=`.spec.slurmNodes.worker.size`,description="The number of worker nodes"
// +kubebuilder:printcolumn:name="Login",type=integer,JSONPath=`.spec.slurmNodes.login.size`,description="The number of login nodes"
// +kubebuilder:printcolumn:name="Accounting",type=boolean,JSONPath=`.spec.slurmNodes.accounting.enabled`,description="Whether accounting is enabled"
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
