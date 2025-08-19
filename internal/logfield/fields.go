package logfield

import (
	"nebius.ai/slurm-operator/internal/consts"
)

const (
	SlurmCluster      = consts.Slurm + ".cluster"
	SlurmJailedConfig = consts.Slurm + ".jailedConfig"
)

const (
	ClusterNamespace = SlurmCluster + ".namespace"
	ClusterName      = SlurmCluster + ".name"

	ResourceKind = SlurmCluster + ".resourceKind"
	ResourceName = SlurmCluster + ".resourceName"

	SubResourceKind = SlurmCluster + ".subResourceKind"
	SubResourceName = SlurmCluster + ".subResourceName"

	JailedConfigNamespace = SlurmJailedConfig + ".namespace"
	JailedConfigName      = SlurmJailedConfig + ".name"

	JailedConfigFilesCount = SlurmJailedConfig + ".filesCount"

	JailedConfigNodesLeft = SlurmJailedConfig + ".nodesLeft"
)
