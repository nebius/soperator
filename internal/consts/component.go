package consts

type ComponentType string

const (
	ComponentTypeController = "controller"
	ComponentTypeWorker     = "worker"
)

const (
	ComponentNameController = SlurmPrefix + ComponentTypeController
	ComponentNameWorker     = SlurmPrefix + ComponentTypeWorker
)

var (
	ComponentNameByType = map[ComponentType]string{
		ComponentTypeController: ComponentNameController,
		ComponentTypeWorker:     ComponentNameWorker,
	}
)
