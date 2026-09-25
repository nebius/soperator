package consts

const (
	// Default timeout in seconds for waiting for topology configuration when using ephemeral topology with topology plugin enabled
	DefaultEphemeralTopologyWaitTimeout = int32(180)

	// SlurmdTopologyPath is where worker-init leaves the topology this worker registers into, on
	// the runtime volume it shares with the slurmd container. It holds the bare "topology=..."
	// fragment that the entrypoint passes to slurmd as --conf, not a Slurm config file.
	SlurmdTopologyPath = VolumeMountPathRuntime + "/soperator/slurmd_topology"
)
