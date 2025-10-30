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

var (
	SlurmUserReasonHC              string = "[user_problem]"
	SlurmNodeReasonHC              string = "[node_problem]"
	SlurmNodeComputeMaintenance    string = SlurmNodeReasonHC + " compute_maintenance"
	SlurmNodeReasonKillTaskFailed  string = "Kill task failed"
	SlurmNodeReasonNodeReplacement string = SlurmNodeComputeMaintenance + ": node replacement process"
	SlurmNodeReasonNodeReboot      string = SlurmNodeComputeMaintenance + ": node reboot process"
)

// order of reasons is important, because we use it to determine if node is in maintenance
var SlurmNodeReasonsList = []string{
	SlurmNodeReasonKillTaskFailed,
	SlurmNodeReasonNodeReplacement,
	SlurmNodeReasonNodeReboot,
	SlurmNodeReasonHC,
	SlurmUserReasonHC,
}

const (
	SlurmConfigRawStrategyPatch    = "patch"
	SlurmConfigRawStrategyOverride = "override"
	SlurmTopologyTree              = "topology/tree"
	SlurmTopologyBlock             = "topology/block"
)
