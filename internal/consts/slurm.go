package consts

const (
	Slurm       = "slurm"
	slurmPrefix = Slurm + "-"

	SlurmCluster  = Slurm + "cluster"
	slurmOperator = slurmPrefix + "operator"

	// TODO: we should rename it. It's not only recommended using root user
	SlurmUser              = "root"
	SlurmLogFile           = "/dev/null"
	SlurmDefaultDebugLevel = "verbose"
)

const (
	SlurmNodeReasonHC              string = "[node_problem]"
	SlurmNodeComputeMaintenance    string = SlurmNodeReasonHC + " compute_maintenance"
	SlurmNodeReasonKillTaskFailed  string = "Kill task failed"
	SlurmNodeReasonNodeReplacement string = SlurmNodeComputeMaintenance + ": node replacement process"
	SlurmNodeReasonNodeReboot      string = SlurmNodeComputeMaintenance + ": node reboot process"
)

var SlurmNodeReasonsMap = map[string]struct{}{
	SlurmNodeReasonHC:              {},
	SlurmNodeReasonKillTaskFailed:  {},
	SlurmNodeReasonNodeReplacement: {},
	SlurmNodeReasonNodeReboot:      {},
}

const (
	SlurmConfigRawStrategyPatch    = "patch"
	SlurmConfigRawStrategyOverride = "override"
	SlurmTopologyTree              = "topology/tree"
)
