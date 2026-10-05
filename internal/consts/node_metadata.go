package consts

// EnvNodeRealMemoryBytes carries the byte representation of the RealMemory value
// rendered into slurm.conf for a worker node.
const EnvNodeRealMemoryBytes = "SOPERATOR_NODE_REAL_MEMORY_BYTES"

// Node metadata is prepared by the operator and persisted by the worker.
const (
	EnvNodePrefix       = "SOPERATOR_NODE_"
	EnvNodePlatformTag  = EnvNodePrefix + "PLATFORM_TAG"
	EnvNodePlatformTags = EnvNodePrefix + "PLATFORM_TAGS"
)
