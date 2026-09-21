package values

import (
	"slices"

	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

type PAMSlurmAdopt struct {
	Enabled       bool
	ActionUnknown slurmv1.PAMSlurmAdoptActionUnknown
	ExemptUsers   []string
	ExemptGroups  []string
}

func buildPAMSlurmAdoptFrom(config *slurmv1.PAMSlurmAdopt) PAMSlurmAdopt {
	if config == nil {
		return PAMSlurmAdopt{ActionUnknown: slurmv1.PAMSlurmAdoptActionUnknownNewest}
	}

	actionUnknown := config.ActionUnknown
	if actionUnknown == "" {
		actionUnknown = slurmv1.PAMSlurmAdoptActionUnknownNewest
	}

	return PAMSlurmAdopt{
		Enabled:       ptr.Deref(config.Enabled, false),
		ActionUnknown: actionUnknown,
		ExemptUsers:   slices.Clone(config.ExemptUsers),
		ExemptGroups:  slices.Clone(config.ExemptGroups),
	}
}
