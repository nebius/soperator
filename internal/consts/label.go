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
	LabelValidateKey    = "slurm.nebius.ai/webhook"
	LabelValidateValue  = "true"

	LabelNodeConfiguratorKey   = "slurm.nebius.ai/node-configurator"
	LabelNodeConfiguratorValue = "true"

	LabelSConfigControllerSourceKey   = "sconficontroller.slurm.nebius.ai"
	LabelSConfigControllerSourceValue = "true"

	// Controller type labels
	LabelControllerType            = "slurm.nebius.ai/controller-type"
	LabelControllerTypeMain        = "main"
	LabelControllerTypePlaceholder = "placeholder"

	TopologyLabelPrefix = "topology.nebius.com"
	TierZeroPrefix      = TopologyLabelPrefix + "/tier-0"
	TierOnePrefix       = TopologyLabelPrefix + "/tier-1"

	LabelJailedAggregationKey         = "slurm.nebius.ai/jailed-aggregation"
	LabelJailedAggregationCommonValue = "common"

	AnnotationConfigHash = "slurm.nebius.ai/config-hash"
)
