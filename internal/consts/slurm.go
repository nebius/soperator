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
	SlurmTopologyTree              = "topology/tree"
	SlurmTopologyBlock             = "topology/block"

	// SlurmTopologyDefaultFabric is the default IB fabric / top-of-tree switch name used for
	// NodeSets without an explicit spec.topology.fabric. It preserves the legacy single-root tree.
	SlurmTopologyDefaultFabric = "root"

	// Topology plugin kinds of a named topology in topology.yaml. Unlike SlurmTopologyTree and
	// SlurmTopologyBlock, which are slurm.conf TopologyPlugin values, these name the per-topology
	// attribute keys of topology.yaml.
	SlurmTopologyTypeTree  = "tree"
	SlurmTopologyTypeBlock = "block"

	// SlurmTopologyNodeSetRefAll covers every NodeSet of the cluster in NamedTopology.NodeSetRefs.
	SlurmTopologyNodeSetRefAll = "ALL"

	// EnvEnableMultiTopology gates rendering topology.yaml from spec.topology.topologies. It is
	// enabled unless explicitly set to "false"; with it off, clusters fall back to the legacy
	// single topology.conf regardless of spec.topology.topologies.
	EnvEnableMultiTopology = "SLURM_OPERATOR_ENABLE_MULTI_TOPOLOGY"
)
