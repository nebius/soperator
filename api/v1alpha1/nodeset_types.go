package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// NodeSet is the Schema for the nodesets API
//
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.phase`,description="The phase of Slurm cluster creation."
type NodeSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status NodeSetStatus `json:"status,omitempty"`
	Spec   NodeSetSpec   `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// NodeSetList contains a list of NodeSet
type NodeSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []NodeSet `json:"items"`
}

// NodeSetStatus defines the observed state of SlurmCluster
type NodeSetStatus struct {
	// TODO #487 Add conditions

	// Phase indicates the current phase of the NodeSet lifecycle.
	// Possible values are: PhaseNodeSetReconciling, PhaseNodeSetNotAvailable, PhaseNodeSetAvailable.
	//
	// +kubebuilder:validation:Optional
	Phase *string `json:"phase,omitempty"`
}

const (
	KindNodeSet = "NodeSet"

	PhaseNodeSetReconciling  = "Reconciling"
	PhaseNodeSetNotAvailable = "Not available"
	PhaseNodeSetAvailable    = "Available"
)

// NodeSetSpec defines the desired state of NodeSet
type NodeSetSpec struct {
	// Replicas specifies the number of worker nodes in the NodeSet.
	//
	// Defaults to 1 if not specified.
	//
	// +kubebuilder:default=1
	// +kubebuilder:validation:Optional
	Replicas int32 `json:"replicas,omitempty"`

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

	// PriorityClass defines the priority class for the Slurm worker pods
	//
	// +kubebuilder:validation:Optional
	PriorityClass string `json:"priorityClass,omitempty"`

	// GPU defines the settings related to GPU support for Slurm workers
	//
	// +kubebuilder:validation:Optional
	GPU GPUSpec `json:"gpu,omitempty"`

	// Slurmd defines the Slurm worker daemon configuration
	//
	// +kubebuilder:validation:Required
	Slurmd ContainerSlurmdSpec `json:"slurmd"`

	// Slurmd defines the Slurm munge configuration
	//
	// +kubebuilder:validation:Required
	Munge ContainerMungeSpec `json:"munge"`

	// NodeAttributes specifies additional attributes for Slurm worker nodes.
	//
	// +kubebuilder:validation:Optional
	NodeAttributes WorkerNodeAttributes `json:"nodeAttributes,omitempty"`

	// ConfigMapRefSupervisord defines the config name of Supervisord for the slurmd container.
	// Specifying a custom name allows providing custom config for the Supervisord.
	//
	// If omitted, the default name and values of config will be used.
	//
	// +kubebuilder:validation:Optional
	ConfigMapRefSupervisord string `json:"configMapRefSupervisord,omitempty"`

	// ConfigMapRefSSHD defines the config name of Slurm SSHD.
	// Specifying a custom name allows providing custom config for the Slurm SSHD.
	//
	// If omitted, the default name and values of config will be used.
	//
	// +kubebuilder:validation:Optional
	ConfigMapRefSSHD string `json:"configMapRefSshd,omitempty"`
}

// GPUSpec defines the settings related to GPU support
type GPUSpec struct {
	// Enabled indicates whether GPU support is enabled for the NodeSet.
	//
	// +kubebuilder:validation:Optional
	Enabled bool `json:"enabled,omitempty"`

	// Nvidia contains settings specific to Nvidia GPUs.
	//
	// +kubebuilder:validation:Optional
	Nvidia GPUVendorNvidiaSpec `json:"nvidia,omitempty"`
}

// GPUVendorNvidiaSpec defines settings specific to Nvidia GPUs
type GPUVendorNvidiaSpec struct {
	// GDRCopyEnabled determines whether GDRCopy should be enabled for Nvidia GPUs
	//
	// +kubebuilder:validation:Optional
	GDRCopyEnabled bool `json:"gdrCopyEnabled,omitempty"`
}

// ContainerSlurmdSpec defines the Slurm worker daemon configuration
type ContainerSlurmdSpec struct {
	// Image defines the image used for the Slurm worker container
	//
	// +kubebuilder:validation:Required
	Image Image `json:"image"`

	// Resources define the [corev1.ResourceList] for the `slurmd` container.
	// It includes both usual cpu, memory, etc., as well, as 3rd-party resource specifications.
	//
	// +kubebuilder:validation:Required
	Resources corev1.ResourceList `json:"resources"`

	// CgroupVersion defines the version of the cgroup
	//
	// +kubebuilder:default="v2"
	// +kubebuilder:validation:Enum="v1";"v2"
	// +kubebuilder:validation:Optional
	CgroupVersion string `json:"cgroupVersion,omitempty"`

	// Volumes define the volume configurations for the Slurm worker container
	//
	// +kubebuilder:validation:Required
	Volumes WorkerVolumesSpec `json:"volumes"`

	// Port defines the port the container exposes
	//
	// +kubebuilder:validation:Optional
	Port int32 `json:"port,omitempty"`

	// Security defines the security configuration for the container
	//
	// +kubebuilder:validation:Optional
	Security ContainerSecuritySpec `json:"security,omitempty"`
}

// WorkerNodeAttributes defines the additional attributes for the NodeSet nodes
type WorkerNodeAttributes struct {
	// Extra defines the string that will be set to the "Extra" field of the corresponding Slurm node.
	// It can use any environment variables that are available in the slurmd container when it starts.
	//
	// +kubebuilder:validation:Optional
	Extra string `json:"extra,omitempty"`

	// Features defines the list of Slurm Node "Features" to filter nodes during job scheduling
	//
	// +kubebuilder:validation:Optional
	Features []string `json:"features,omitempty"`
}

// ContainerMungeSpec defines the Slurm munge configuration
type ContainerMungeSpec struct {
	// Image defines the image used for the Slurm munge container
	//
	// +kubebuilder:validation:Required
	Image Image `json:"image"`

	// Resources define the [corev1.ResourceList] for the Slurm munge container.
	//
	// +kubebuilder:validation:Required
	Resources corev1.ResourceList `json:"resources,omitempty"`

	// Security defines the security configuration for the container
	//
	// +kubebuilder:validation:Optional
	Security ContainerSecuritySpec `json:"security,omitempty"`
}

// WorkerVolumesSpec defines the volumes for the Slurm worker container
type WorkerVolumesSpec struct {
	// Jail represents the jail data volume configuration
	//
	// +kubebuilder:validation:Required
	Jail corev1.VolumeSource `json:"jail"`

	// JailSubMounts defines the configuration of volume mounts within the jail volume
	//
	// +kubebuilder:validation:Required
	JailSubMounts []NodeVolumeJailSubMount `json:"jailSubMounts"`

	// SharedMemorySize defines the size of the shared memory (/dev/shm)
	//
	// +kubebuilder:default="64Gi"
	SharedMemorySize *resource.Quantity `json:"sharedMemorySize,omitempty"`
}

// NodeVolumeJailSubMount defines the configuration of volume mount within the jail volume
type NodeVolumeJailSubMount struct {
	// Name defines the name of the sub-mount
	//
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// MountPath defines the path where the sub-mount is mounted
	//
	// +kubebuilder:validation:Required
	MountPath string `json:"mountPath"`

	// SubPath defines a sub-path inside the referenced VolumeSource or VolumeClaimTemplateSpec instead of its root.
	// Corresponds to the [corev1.VolumeMount.SubPath] field.
	//
	// See: https://kubernetes.io/docs/concepts/storage/volumes/#using-subpath
	//
	// +kubebuilder:validation:Optional
	SubPath string `json:"subPath,omitempty"`

	// ReadOnly defines whether the mounting point should be read-only
	//
	// +kubebuilder:validation:Optional
	ReadOnly bool `json:"readOnly,omitempty"`

	// VolumeSource defines the volume source for the sub-mount
	//
	// +kubebuilder:validation:Optional
	VolumeSource *corev1.VolumeSource `json:"volumeSource,omitempty"`

	// VolumeClaimTemplateSpec defines the [corev1.PersistentVolumeClaim] template specification
	//
	// +kubebuilder:validation:Optional
	VolumeClaimTemplateSpec *corev1.PersistentVolumeClaimSpec `json:"volumeClaimTemplateSpec,omitempty"`
}

func init() {
	SchemeBuilder.Register(&NodeSet{}, &NodeSetList{})
}
