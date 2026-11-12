package values

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

func TestBuildSlurmLoginFrom_SSSD(t *testing.T) {
	t.Run("uses default secret name when ref is empty", func(t *testing.T) {
		login := &slurmv1.SlurmNodeLogin{
			SlurmNode: slurmv1.SlurmNode{},
			Sshd:      slurmv1.NodeContainer{Image: "sshd-image"},
			Munge:     slurmv1.NodeContainer{Image: "munge-image"},
			SidecarSSSD: slurmv1.SidecarSSSD{
				Sssd: &slurmv1.NodeContainer{Image: "sssd-image"},
			},
			Volumes: slurmv1.SlurmNodeLoginVolumes{Jail: slurmv1.NodeVolume{}},
		}

		result := buildSlurmLoginFrom("test-cluster", "", nil, login)

		if assert.NotNil(t, result.ContainerSSSD) {
			assert.Equal(t, "sssd-image", result.ContainerSSSD.Image)
			assert.Equal(t, consts.ContainerNameSSSD, result.ContainerSSSD.Name)
		}
		assert.True(t, result.IsSSSDSecretDefault)
		assert.Equal(t, naming.BuildSecretSSSDConfName("test-cluster"), result.SSSDConfSecretName)
	})

	t.Run("uses referenced secret name when ref is set", func(t *testing.T) {
		login := &slurmv1.SlurmNodeLogin{
			Sshd:  slurmv1.NodeContainer{Image: "sshd-image"},
			Munge: slurmv1.NodeContainer{Image: "munge-image"},
			SidecarSSSD: slurmv1.SidecarSSSD{
				Sssd:                  &slurmv1.NodeContainer{Image: "sssd-image"},
				SSSDConfSecretRefName: "custom-sssd-secret",
			},
			Volumes: slurmv1.SlurmNodeLoginVolumes{Jail: slurmv1.NodeVolume{}},
		}

		result := buildSlurmLoginFrom("test-cluster", "", nil, login)

		assert.False(t, result.IsSSSDSecretDefault)
		assert.Equal(t, "custom-sssd-secret", result.SSSDConfSecretName)
	})

	t.Run("keeps sssd disabled when container is not configured", func(t *testing.T) {
		login := &slurmv1.SlurmNodeLogin{
			Sshd:    slurmv1.NodeContainer{Image: "sshd-image"},
			Munge:   slurmv1.NodeContainer{Image: "munge-image"},
			Volumes: slurmv1.SlurmNodeLoginVolumes{Jail: slurmv1.NodeVolume{}},
		}

		result := buildSlurmLoginFrom("test-cluster", "", nil, login)

		assert.Nil(t, result.ContainerSSSD)
		assert.Equal(t, naming.BuildSecretSSSDConfName("test-cluster"), result.SSSDConfSecretName)
		assert.True(t, result.IsSSSDSecretDefault)
	})
}

func TestBuildSlurmLoginFrom_Docker(t *testing.T) {
	tests := []struct {
		name    string
		docker  *slurmv1.LoginDocker
		enabled bool
	}{
		{name: "object omitted"},
		{name: "enabled omitted", docker: &slurmv1.LoginDocker{}},
		{name: "explicitly disabled", docker: &slurmv1.LoginDocker{Enabled: ptr.To(false)}},
		{name: "enabled", docker: &slurmv1.LoginDocker{Enabled: ptr.To(true)}, enabled: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			login := &slurmv1.SlurmNodeLogin{
				Sshd:    slurmv1.NodeContainer{Image: "sshd-image"},
				Munge:   slurmv1.NodeContainer{Image: "munge-image"},
				Volumes: slurmv1.SlurmNodeLoginVolumes{Jail: slurmv1.NodeVolume{}},
				Docker:  test.docker,
			}

			result := buildSlurmLoginFrom("test-cluster", "", nil, login)

			assert.Equal(t, test.enabled, result.DockerEnabled)
		})
	}
}

func TestBuildSlurmLoginFrom_PreservesSSSHDConfigDefault(t *testing.T) {
	login := &slurmv1.SlurmNodeLogin{
		Sshd:    slurmv1.NodeContainer{Image: "sshd-image"},
		Munge:   slurmv1.NodeContainer{Image: "munge-image"},
		Volumes: slurmv1.SlurmNodeLoginVolumes{Jail: slurmv1.NodeVolume{}},
	}

	result := buildSlurmLoginFrom("test-cluster", "", nil, login)

	assert.Equal(t, naming.BuildConfigMapSSHDConfigsNameLogin("test-cluster"), result.SSHDConfigMapName)
	assert.True(t, result.IsSSHDConfigMapDefault)
	assert.Equal(t, corev1.ServiceTypeClusterIP, result.HeadlessService.Type)
}

func TestBuildSlurmLoginFromUsesConfiguredStatefulSetNamePrefix(t *testing.T) {
	login := &slurmv1.SlurmNodeLogin{
		Sshd:    slurmv1.NodeContainer{Image: "sshd-image"},
		Munge:   slurmv1.NodeContainer{Image: "munge-image"},
		Volumes: slurmv1.SlurmNodeLoginVolumes{Jail: slurmv1.NodeVolume{}},
	}

	prefixed := buildSlurmLoginFrom("test-cluster", "test-cluster", nil, login)
	unprefixed := buildSlurmLoginFrom("test-cluster", "", nil, login)

	assert.Equal(t, "test-cluster-login", prefixed.StatefulSet.Name)
	assert.Equal(t, consts.ComponentTypeLogin.String(), unprefixed.StatefulSet.Name)
}

func TestBuildSlurmLoginFromUpdateStrategy(t *testing.T) {
	tests := []struct {
		name       string
		configured consts.UpdateStrategy
		expected   consts.UpdateStrategy
	}{
		{"omitted defaults to delayed rollout", "", consts.UpdateStrategyDelayedRollout},
		{"explicit delayed rollout", consts.UpdateStrategyDelayedRollout, consts.UpdateStrategyDelayedRollout},
		{"automatic rolling updates", consts.UpdateStrategyRollingUpdate, consts.UpdateStrategyRollingUpdate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			login := &slurmv1.SlurmNodeLogin{UpdateStrategy: tt.configured}
			result := buildSlurmLoginFrom("test-cluster", "", nil, login)
			assert.Equal(t, tt.expected, result.UpdateStrategy)
			assert.Equal(t, tt.configured, login.UpdateStrategy, "value construction must not mutate the API object")
		})
	}
}

func TestResolveLoginUpdatePolicy(t *testing.T) {
	tests := []struct {
		name     string
		config   *slurmv1.LoginUpdatePolicy
		notice   time.Duration
		schedule string
	}{
		{"omitted policy", nil, 30 * 24 * time.Hour, ""},
		{"empty policy", &slurmv1.LoginUpdatePolicy{}, 30 * 24 * time.Hour, ""},
		{
			"notice override", &slurmv1.LoginUpdatePolicy{MinNotice: &metav1.Duration{Duration: time.Hour}},
			time.Hour, "",
		},
		{
			"schedule override", &slurmv1.LoginUpdatePolicy{Schedule: ptr.To("0 3 * * *")},
			30 * 24 * time.Hour, "0 3 * * *",
		},
		{
			"zero notice", &slurmv1.LoginUpdatePolicy{MinNotice: &metav1.Duration{}},
			0, "",
		},
		{
			"timezone and surrounding whitespace", &slurmv1.LoginUpdatePolicy{Schedule: ptr.To("  CRON_TZ=Europe/Amsterdam 0 3 * * 2  ")},
			30 * 24 * time.Hour, "CRON_TZ=Europe/Amsterdam 0 3 * * 2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := tt.config.DeepCopy()
			notice, schedule, err := ResolveLoginUpdatePolicy(tt.config)
			require.NoError(t, err)
			assert.Equal(t, tt.notice, notice)
			if tt.schedule == "" {
				assert.Nil(t, schedule, "an omitted schedule must not enable cron scheduling")
			} else {
				assert.NotNil(t, schedule)
			}
			assert.Equal(t, original, tt.config, "resolving defaults must not mutate the API object")
		})
	}
}

func TestResolveLoginUpdatePolicyRejectsInvalidConfiguration(t *testing.T) {
	for _, spec := range []string{
		"", "not a schedule", "@every 1m", "0 0 0 1 * *",
		"CRON_TZ=UTC", "TZ=UTC", "CRON_TZ= 0 0 1 * *", "TZ= 0 0 1 * *",
	} {
		t.Run(spec, func(t *testing.T) {
			_, _, err := ResolveLoginUpdatePolicy(&slurmv1.LoginUpdatePolicy{Schedule: ptr.To(spec)})
			require.Error(t, err)
		})
	}
}
