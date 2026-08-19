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
	// SlurmTopologyTypeFlat is never selectable in the CRD; it names the generated CPU-only topology.
	SlurmTopologyTypeFlat = "flat"

	// SlurmTopologyNodeSetRefAll covers every NodeSet of the cluster in NamedTopology.NodeSetRefs.
	// CPU-only NodeSets are excluded: they belong to the generated flat topology instead.
	SlurmTopologyNodeSetRefAll = "ALL"

	// SlurmTopologyCPUOnlyName is the topology the operator generates for CPU-only NodeSets. They
	// have no fabric to optimize placement on, so a flat topology states that plainly instead of
	// hanging them off a fabricated switch inside somebody else's IB tree.
	SlurmTopologyCPUOnlyName = "cpu"

	// EnvEnableMultiTopology gates rendering topology.yaml from spec.topology.topologies. It is
	// enabled unless explicitly set to "false"; with it off, clusters fall back to the legacy
	// single topology.conf regardless of spec.topology.topologies.
	EnvEnableMultiTopology = "SLURM_OPERATOR_ENABLE_MULTI_TOPOLOGY"
)
