package consts

const (
	Slurm       = "slurm"
	slurmPrefix = Slurm + "-"

	SlurmCluster  = Slurm + "cluster"
	slurmOperator = slurmPrefix + "operator"

	// TODO: we should rename it. It's not only recommended using root user
	SlurmUser              = "root"
	SlurmLogFile           = "/dev/null"
	SlurmDefaultDebugLevel = "info"
)

const (
	SlurmNodeReasonHC              string = "[HC]"
	SlurmNodeReasonKillTaskFailed  string = "Kill task failed"
	SlurmNodeReasonNodeReplacement string = "Soperator auto-healing: node replacement process"
	SlurmNodeReasonNodeReboot      string = "Soperator auto-healing: node reboot process"
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
