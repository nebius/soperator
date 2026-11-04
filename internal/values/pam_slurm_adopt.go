package values

import (
	"slices"

	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

type PAMSlurmAdopt struct {
	Enabled      bool
	ExemptUsers  []string
	ExemptGroups []string
}

func buildPAMSlurmAdoptFrom(config *slurmv1.PAMSlurmAdopt) PAMSlurmAdopt {
	if config == nil {
		return PAMSlurmAdopt{Enabled: false}
	}

	return PAMSlurmAdopt{
		Enabled:      ptr.Deref(config.Enabled, false),
		ExemptUsers:  slices.Clone(config.ExemptUsers),
		ExemptGroups: slices.Clone(config.ExemptGroups),
	}
}
