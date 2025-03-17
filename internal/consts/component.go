package consts

// This is expected behaviour of enum. Standard `type ComponentType string`
// makes it possible to pass any string value to fields expecting ComponentType.

// ComponentType is an enum of Slurm component types
type ComponentType interface {
	ct()
	String() string
}

type baseComponentType struct {
	value string
}

func (b baseComponentType) ct() {}
func (b baseComponentType) String() string {
	return b.value
}

var (
	ComponentTypeCommon            ComponentType = baseComponentType{"common"}
	ComponentTypeController        ComponentType = baseComponentType{"controller"}
	ComponentTypeAccounting        ComponentType = baseComponentType{"accounting"}
	ComponentTypeREST              ComponentType = baseComponentType{"rest"}
	ComponentTypeWorker            ComponentType = baseComponentType{"worker"}
	ComponentTypeNodeConfigurator  ComponentType = baseComponentType{"node-configurator"}
	ComponentTypeLogin             ComponentType = baseComponentType{"login"}
	ComponentTypeBenchmark         ComponentType = baseComponentType{"nccl-benchmark"}
	ComponentTypePopulateJail      ComponentType = baseComponentType{"populate-jail"}
	ComponentTypeExporter          ComponentType = baseComponentType{"exporter"}
	ComponentTypeMariaDbOperator   ComponentType = baseComponentType{"mariadb-operator"}
	ComponentTypeSConfigController ComponentType = baseComponentType{"sconfigcontroller"}
)
