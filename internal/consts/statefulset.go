package consts

const (
	PodManagementPolicy = "Parallel"
)

type UpdateStrategy string

const (
	UpdateStrategyDelayedRollout          UpdateStrategy = "delayedRollout"
	UpdateStrategyRollingUpdate           UpdateStrategy = "rollingUpdate"
	UpdateStrategySlurmAwareRollingUpdate UpdateStrategy = "slurmAwareRollingUpdate"
)
