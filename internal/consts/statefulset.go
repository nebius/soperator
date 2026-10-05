package consts

const (
	PodManagementPolicy = "Parallel"
)

type UpdateStrategy string

const (
	UpdateStrategyRollingUpdate           UpdateStrategy = "rollingUpdate"
	UpdateStrategySlurmAwareRollingUpdate UpdateStrategy = "slurmAwareRollingUpdate"
)
