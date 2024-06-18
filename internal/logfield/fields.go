package logfield

import (
	"nebius.ai/slurm-operator/internal/consts"
)

const (
	SlurmCluster = consts.Slurm + ".cluster"
)

const (
	Namespace   = SlurmCluster + ".namespace"
	ClusterName = SlurmCluster + ".name"

	ResourceName = SlurmCluster + ".resourceName"
	ResourceKind = SlurmCluster + ".resourceKind"
)
