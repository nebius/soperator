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

const (
	SlurmNodeReasonKillTaskFailed       string = "Kill task failed"
	SlurmNodeReasonMaintenanceScheduled string = "Maintenance scheduled"
	SlurmNodeReasonDegraded             string = "Compute node is degraded"
)

var SlurmNodeReasonsMap = map[string]struct{}{
	SlurmNodeReasonKillTaskFailed:       {},
	SlurmNodeReasonMaintenanceScheduled: {},
	SlurmNodeReasonDegraded:             {},
}
