package consts

const (
	ServiceControllerName = "slurmctld"
	ServiceWorkerName     = "slurmd"
)

const (
	StatefulSetControllerName = ServiceControllerName
	StatefulSetWorkerName     = ServiceWorkerName
)

const (
	ContainerControllerName = ServiceControllerName
	ContainerWorkerName     = ServiceWorkerName
)
