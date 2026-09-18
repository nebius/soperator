package resourcegetter

import slurmv1 "nebius.ai/slurm-operator/api/v1"

// ResolveWorkloadNamePrefix returns the prefix to use for workload resource names
//
// An empty mode is treated as enabled for objects created before the field was introduced.
func ResolveWorkloadNamePrefix(clusterName string, mode slurmv1.WorkloadNamePrefixMode) string {
	if mode == slurmv1.WorkloadNamePrefixDisabled {
		return ""
	}

	return clusterName
}

func BuildPrefixedName(prefix, base string) string {
	if prefix == "" {
		return base
	}
	return prefix + "-" + base
}
