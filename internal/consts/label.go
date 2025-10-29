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

	LabelNodeSetKey = K8sGroupNameSoperator + "/nodeset"

	LabelSConfigControllerSourceKey   = "sconfigcontroller." + K8sGroupNameSoperator
	LabelSConfigControllerSourceValue = "true"

	// Controller type labels
	LabelControllerType            = K8sGroupNameSoperator + "/controller-type"
	LabelControllerTypeMain        = "main"
	LabelControllerTypePlaceholder = "placeholder"

	DefaultTopologyLabelPrefix = "topology.nebius.com"
	TierZeroSuffix             = "/tier-0"
	TierOneSuffix              = "/tier-1"

	LabelJailedAggregationKey         = K8sGroupNameSoperator + "/jailed-aggregation"
	LabelJailedAggregationCommonValue = "common"

	AnnotationConfigHash = K8sGroupNameSoperator + "/config-hash"
)
