package values

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func TestBuildNamespaceConfigFrom(t *testing.T) {
	tests := []struct {
		name   string
		config *slurmv1.NamespaceConfig
		want   NamespaceConfig
	}{
		{
			name: "absent configuration enables generated namespaces",
			want: NamespaceConfig{Enabled: true},
		},
		{
			name:   "empty configuration enables generated namespaces",
			config: &slurmv1.NamespaceConfig{},
			want:   NamespaceConfig{Enabled: true},
		},
		{
			name:   "explicit enablement generates namespaces",
			config: &slurmv1.NamespaceConfig{Enabled: ptr.To(true)},
			want:   NamespaceConfig{Enabled: true},
		},
		{
			name:   "explicit disablement disables namespaces",
			config: &slurmv1.NamespaceConfig{Enabled: ptr.To(false)},
			want:   NamespaceConfig{},
		},
		{
			name: "custom configuration enables namespaces by default",
			config: &slurmv1.NamespaceConfig{
				ConfigMapRef: &slurmv1.NamespaceConfigMapReference{Name: "custom-namespace"},
			},
			want: NamespaceConfig{Enabled: true, ConfigMapName: "custom-namespace"},
		},
		{
			name: "explicit enablement uses custom configuration",
			config: &slurmv1.NamespaceConfig{
				Enabled:      ptr.To(true),
				ConfigMapRef: &slurmv1.NamespaceConfigMapReference{Name: "custom-namespace"},
			},
			want: NamespaceConfig{Enabled: true, ConfigMapName: "custom-namespace"},
		},
		{
			name: "disablement takes precedence over custom configuration",
			config: &slurmv1.NamespaceConfig{
				Enabled:      ptr.To(false),
				ConfigMapRef: &slurmv1.NamespaceConfigMapReference{Name: "custom-namespace"},
			},
			want: NamespaceConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildNamespaceConfigFrom(tt.config))
		})
	}
}
