package values

import (
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

type NamespaceConfig struct {
	Enabled       bool
	ConfigMapName string
}

func buildNamespaceConfigFrom(config *slurmv1.NamespaceConfig) NamespaceConfig {
	result := NamespaceConfig{Enabled: true}
	if config == nil {
		return result
	}

	result.Enabled = ptr.Deref(config.Enabled, true)
	if result.Enabled && config.ConfigMapRef != nil {
		result.ConfigMapName = config.ConfigMapRef.Name
	}
	return result
}
