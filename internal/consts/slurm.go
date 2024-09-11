package consts

const (
	Slurm       = "slurm"
	slurmPrefix = Slurm + "-"

	SlurmCluster  = Slurm + "cluster"
	slurmOperator = slurmPrefix + "operator"

	// TODO: we should rename it. It's not only recommended using root user
	SlurmUser              = "root"
	SlurmLogFile           = "/dev/null"
	SlurmDefaultDebugLevel = "debug3"
)
