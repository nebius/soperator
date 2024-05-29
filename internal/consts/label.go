package consts

const (
	LabelNameKey   = "app.kubernetes.io/name"
	LabelNameValue = "SlurmCluster"

	// LabelInstanceKey value is taken from the corresponding CRD
	LabelInstanceKey = "app.kubernetes.io/instance"

	// LabelComponentKey value is taken from the corresponding CRD
	LabelComponentKey = "app.kubernetes.io/component"

	LabelPartOfKey   = "app.kubernetes.io/part-of"
	LabelPartOfValue = LabelNameValue

	LabelManagedByKey   = "app.kubernetes.io/managed-by"
	LabelManagedByValue = SlurmPrefix + "operator"
)
