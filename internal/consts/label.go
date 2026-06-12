package consts

const (
	LabelNameKey   = "app.kubernetes.io/name"
	LabelNameValue = SlurmCluster

	LabelNameExporterValue = "slurm-exporter"

	// LabelInstanceKey value is taken from the corresponding CRD
	LabelInstanceKey = "app.kubernetes.io/instance"

	// LabelComponentKey value is taken from the corresponding CRD
	LabelComponentKey = "app.kubernetes.io/component"

	LabelPartOfKey   = "app.kubernetes.io/part-of"
	LabelPartOfValue = slurmOperator

	LabelManagedByKey   = "app.kubernetes.io/managed-by"
	LabelManagedByValue = slurmOperator
	LabelValidateKey    = K8sGroupNameSoperator + "/webhook"
	LabelValidateValue  = "true"

	LabelNodeConfiguratorKey   = K8sGroupNameSoperator + "/node-configurator"
	LabelNodeConfiguratorValue = "true"

	LabelNodeSetKey  = K8sGroupNameSoperator + "/nodeset"
	LabelWorkerKey   = K8sGroupNameSoperator + "/worker"
	LabelWorkerValue = "true"

	LabelSConfigControllerSourceKey   = "sconfigcontroller." + K8sGroupNameSoperator
	LabelSConfigControllerSourceValue = "true"

	// Controller type labels
	LabelControllerType     = K8sGroupNameSoperator + "/controller-type"
	LabelControllerTypeMain = "main"

	DefaultTopologyLabelPrefix = "topology.nebius.com"
	TierZeroSuffix             = "/tier-0"
	TierOneSuffix              = "/tier-1"
	// GPUClusterIDSuffix is appended to the topology label prefix to form the K8s node label
	// (e.g. "topology.nebius.com/gpu-cluster-id") whose value names the IB fabric / root switch
	// a node belongs to.
	GPUClusterIDSuffix = "/gpu-cluster-id"

	// TopologyKeyGPUClusterID is the reserved key under which the gpu-cluster-id value is stored
	// in the per-node topology labels map (alongside "tier-N" keys). It is not a tier.
	TopologyKeyGPUClusterID = "gpu-cluster-id"

	LabelJailedAggregationKey         = K8sGroupNameSoperator + "/jailed-aggregation"
	LabelJailedAggregationCommonValue = "common"

	AnnotationConfigHash = K8sGroupNameSoperator + "/config-hash"
)
