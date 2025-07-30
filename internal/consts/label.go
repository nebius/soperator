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

	TopologyLabelPrefix = "topology.nebius.com"
	TierOnePrefix       = TopologyLabelPrefix + "/tier-1"
)
