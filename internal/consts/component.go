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
	ComponentTypeController ComponentType = baseComponentType{"controller"}
	ComponentTypeWorker     ComponentType = baseComponentType{"worker"}
	ComponentTypeLogin      ComponentType = baseComponentType{"login"}
	ComponentTypeBenchmark  ComponentType = baseComponentType{"nccl-benchmark"}
)
