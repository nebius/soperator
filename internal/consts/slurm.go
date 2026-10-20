package consts

const (
	Slurm       = "slurm"
	slurmPrefix = Slurm + "-"

	SlurmCluster  = Slurm + "cluster"
	slurmOperator = slurmPrefix + "operator"

	// TODO: we should rename it. It's not only recommended using root user
	SlurmUser              = "root"
	SlurmLogFile           = "/dev/null"
	SlurmDefaultDebugLevel = "debug"

	// SlurmDefaultResumeTimeout mirrors the CRD default of SlurmConfig.ResumeTimeout, in seconds.
	SlurmDefaultResumeTimeout = 1800

	// SlurmDefaultSuspendTimeout mirrors the CRD default, in seconds.
	SlurmDefaultSuspendTimeout = 90

	// SlurmDefaultLicenses is rendered as Licenses= into slurm.conf. Active checks request these
	// licenses as cluster-wide semaphores: every gpu-checks job takes gpu_checks:1, so the count
	// bounds how many nodes run that check at once. A Licenses= line in customSlurmConfig overrides it.
	SlurmDefaultLicenses = "gpu_checks:200"

	SlurmPowerActionWorkerHandoff = "soperator-worker-handoff"
)

var (
	SlurmUserReasonHC              string = "[user_problem]"
	SlurmNodeReasonHC              string = "[node_problem]"
	SlurmHardwareReasonHC          string = "[hardware_problem]"
	SlurmNodeComputeMaintenance    string = "[compute_maintenance]"
	SlurmNodeReasonKillTaskFailed  string = "Kill task failed"
	SlurmNodeReasonNodeReplacement string = SlurmNodeComputeMaintenance + " node replacement process"
	SlurmNodeReasonNodeReboot      string = SlurmNodeComputeMaintenance + " node reboot process"
)

// order of reasons is important, because we use it to determine if node is in maintenance
var SlurmNodeReasonsList = []string{
	SlurmNodeReasonKillTaskFailed,
	SlurmNodeReasonNodeReplacement,
	SlurmNodeReasonNodeReboot,
	SlurmNodeReasonHC,
	SlurmUserReasonHC,
	SlurmHardwareReasonHC,
}

const (
	SlurmConfigRawStrategyPatch    = "patch"
	SlurmConfigRawStrategyOverride = "override"
	// SlurmTopologyDefaultFabric is the default IB fabric / top-of-tree switch name used for
	// NodeSets without an explicit spec.topology.fabric. It preserves the legacy single-root tree.
	SlurmTopologyDefaultFabric = "root"

	// Topology plugin kinds of a named topology in topology.yaml. These name the per-topology
	// attribute keys rather than slurm.conf values.
	SlurmTopologyTypeTree  = "tree"
	SlurmTopologyTypeBlock = "block"
	SlurmTopologyTypeFlat  = "flat"

	// SlurmTopologyNodeSetRefAll covers every NodeSet of the cluster in NamedTopology.NodeSetRefs.
	SlurmTopologyNodeSetRefAll = "ALL"
)
