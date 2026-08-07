package consts

// EnvNodeRealMemoryBytes carries the byte representation of the RealMemory value
// rendered into slurm.conf for a worker node.
const EnvNodeRealMemoryBytes = "SOPERATOR_NODE_REAL_MEMORY_BYTES"

// EnvSlurmNodeName carries the Slurm node name rendered from the pod name.
const EnvSlurmNodeName = "SLURM_NODE_NAME"
