package consts

import (
	_ "embed"
	"strings"
)

const (
	LabelNameKey   string = "app.kubernetes.io/name"
	LabelNameValue string = "SlurmCluster"

	// LabelInstanceKey value is taken from the corresponding CRD
	LabelInstanceKey string = "app.kubernetes.io/instance"

	// LabelVersionKey value is taken from chart version.
	// See LabelVersionValue
	LabelVersionKey string = "app.kubernetes.io/version"

	// LabelComponentKey value is taken from the corresponding CRD
	LabelComponentKey string = "app.kubernetes.io/component"

	LabelPartOfKey   string = "app.kubernetes.io/part-of"
	LabelPartOfValue string = LabelNameValue

	LabelManagedByKey   string = "app.kubernetes.io/managed-by"
	LabelManagedByValue string = SlurmPrefix + "operator"
)

var (
	LabelVersionValue = strings.TrimSpace(versionValue)
	//go:embed v/version.txt
	versionValue string
)
