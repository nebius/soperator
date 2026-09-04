package e2e

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validProfile() Profile {
	return Profile{
		NebiusProjectID: "project-123",
		NebiusRegion:    "eu-north1",
		NebiusTenantID:  "tenant-456",
		NodeSets: []NodeSetDef{
			{
				Name:             "worker-gpu",
				Platform:         "gpu-h100-sxm",
				Preset:           "8gpu-128vcpu-1600gb",
				Size:             2,
				InfinibandFabric: "cuda",
			},
		},
	}
}

func TestValidate_Valid(t *testing.T) {
	p := validProfile()
	assert.NoError(t, p.Validate())
}

func TestValidate_MultipleNodeSets(t *testing.T) {
	p := validProfile()
	p.NodeSets = append(p.NodeSets, NodeSetDef{
		Name:     "worker-cpu",
		Platform: "cpu",
		Preset:   "16vcpu-64gb",
		Size:     3,
	})
	assert.NoError(t, p.Validate())
}

func TestValidate_EmptyNodeSets(t *testing.T) {
	p := validProfile()
	p.NodeSets = nil
	assert.ErrorContains(t, p.Validate(), "nodesets must not be empty")
}

func TestValidate_DuplicateNames(t *testing.T) {
	p := validProfile()
	p.NodeSets = append(p.NodeSets, p.NodeSets[0])
	assert.ErrorContains(t, p.Validate(), "duplicate name")
}

func TestValidate_MissingName(t *testing.T) {
	p := validProfile()
	p.NodeSets[0].Name = ""
	assert.ErrorContains(t, p.Validate(), "name is required")
}

func TestValidate_MissingPlatform(t *testing.T) {
	p := validProfile()
	p.NodeSets[0].Platform = ""
	assert.ErrorContains(t, p.Validate(), "platform is required")
}

func TestValidate_MissingPreset(t *testing.T) {
	p := validProfile()
	p.NodeSets[0].Preset = ""
	assert.ErrorContains(t, p.Validate(), "preset is required")
}

func TestValidate_ZeroSize(t *testing.T) {
	p := validProfile()
	p.NodeSets[0].Size = 0
	assert.ErrorContains(t, p.Validate(), "size must be positive")
}

func TestValidate_NegativeSize(t *testing.T) {
	p := validProfile()
	p.NodeSets[0].Size = -1
	assert.ErrorContains(t, p.Validate(), "size must be positive")
}

func TestValidate_CapacityStrategyDefaultsToWarn(t *testing.T) {
	p := validProfile()
	require.NoError(t, p.Validate())
	assert.Equal(t, CapacityStrategyWarn, p.CapacityStrategy)
}

func TestValidate_CapacityStrategyWarn(t *testing.T) {
	p := validProfile()
	p.CapacityStrategy = CapacityStrategyWarn
	require.NoError(t, p.Validate())
	assert.Equal(t, CapacityStrategyWarn, p.CapacityStrategy)
}

func TestValidate_CapacityStrategyCancel(t *testing.T) {
	p := validProfile()
	p.CapacityStrategy = CapacityStrategyCancel
	require.NoError(t, p.Validate())
	assert.Equal(t, CapacityStrategyCancel, p.CapacityStrategy)
}

func TestValidate_CapacityStrategyUnknown(t *testing.T) {
	p := validProfile()
	p.CapacityStrategy = "unknown"
	assert.ErrorContains(t, p.Validate(), "unknown capacity_strategy")
}

func TestLoadE2EConfig_GB300TerraformOverrides(t *testing.T) {
	raw := `
profiles:
  MAN_GB300:
    nebius_project_id: project-gb300
    nebius_region: eu-north1
    nebius_tenant_id: tenant-gb300
    capacity_strategy: cancel
    labels:
      - gb300
    nodesets:
      - name: worker
        platform: gpu-gb300
        preset: 4gpu-112vcpu-800gb
        size: 2
        infiniband_fabric: gb300-fabric
        terraform_overrides:
          nvlink:
            enabled: true
            type: GB300
          placement_policy_nodes:
            - provider-node-1
            - provider-node-2
          local_nvme:
            enabled: true
            device_count: 4
            device_capacity_gigabytes: 7680
          future_worker_field:
            arbitrary: value
`

	t.Setenv("E2E_CONFIG", raw)
	config, err := LoadE2EConfig()
	require.NoError(t, err)
	profile, ok := config.Profiles["MAN_GB300"]
	require.True(t, ok)
	assert.False(t, profile.HasLabel(autoSelectLabel))
	require.Len(t, profile.NodeSets, 1)
	wantOverrides := map[string]interface{}{
		"nvlink": map[string]interface{}{
			"enabled": true,
			"type":    "GB300",
		},
		"placement_policy_nodes": []interface{}{"provider-node-1", "provider-node-2"},
		"local_nvme": map[string]interface{}{
			"enabled":                   true,
			"device_count":              float64(4),
			"device_capacity_gigabytes": float64(7680),
		},
		"future_worker_field": map[string]interface{}{
			"arbitrary": "value",
		},
	}
	assert.Equal(t, wantOverrides, profile.NodeSets[0].TerraformOverrides)

	roundTrip, err := profile.YAML()
	require.NoError(t, err)
	reparsed, err := ParseProfile(roundTrip)
	require.NoError(t, err)
	assert.Equal(t, wantOverrides, reparsed.NodeSets[0].TerraformOverrides)
}

func TestParseProfile_LegacyGPUProfiles(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		preset   string
	}{
		{name: "H100", platform: "gpu-h100-sxm", preset: "8gpu-128vcpu-1600gb"},
		{name: "H200", platform: "gpu-h200-sxm", preset: "8gpu-128vcpu-1600gb"},
		{name: "B200", platform: "gpu-b200-sxm", preset: "8gpu-224vcpu-1800gb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := fmt.Sprintf(`
nodesets:
  - name: worker
    platform: %s
    preset: %s
    size: 2
`, tt.platform, tt.preset)

			profile, err := ParseProfile(raw)
			require.NoError(t, err)
			require.Len(t, profile.NodeSets, 1)
			assert.Equal(t, tt.platform, profile.NodeSets[0].Platform)
			assert.Equal(t, tt.preset, profile.NodeSets[0].Preset)
			assert.Nil(t, profile.NodeSets[0].TerraformOverrides)

			roundTrip, err := profile.YAML()
			require.NoError(t, err)
			assert.NotContains(t, roundTrip, "terraform_overrides")
		})
	}
}

func TestNormalizeProfileName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"MAN_H100", "MAN_H100"},
		{"man_h100", "MAN_H100"},
		{"  man_h100  ", "MAN_H100"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeProfileName(tt.in))
		})
	}
}

// autoSelectLabel mirrors the label the real E2E_CONFIG puts on auto-selectable
// profiles. It means nothing to the code under test — any label would do.
const autoSelectLabel = "auto-select"

func validConfig() E2EConfig {
	labelled := validProfile()
	labelled.Labels = []string{autoSelectLabel}
	return E2EConfig{
		Profiles: map[string]Profile{"MAN_H100": labelled},
	}
}

func TestConfigValidate_Valid(t *testing.T) {
	s := validConfig()
	assert.NoError(t, s.Validate())
}

func TestConfigValidate_EmptyProfiles(t *testing.T) {
	s := E2EConfig{}
	assert.ErrorContains(t, s.Validate(), "profiles must not be empty")
}

func TestConfigValidate_BadProfileName(t *testing.T) {
	for _, name := range []string{
		"man-h100",  // lowercase and a hyphen
		"100_H100",  // leading digit
		"MAN H100",  // space
		"@MAN_H100", // label marker
		"_MAN_H100", // leading underscore
		"",          // empty
	} {
		t.Run(name, func(t *testing.T) {
			s := validConfig()
			s.Profiles[name] = validProfile()
			assert.ErrorContains(t, s.Validate(), "name must match")
		})
	}
}

func TestConfigValidate_ProfileErrorNamesTheProfile(t *testing.T) {
	s := validConfig()
	broken := validProfile()
	broken.NodeSets = nil
	s.Profiles["BROKEN"] = broken
	assert.ErrorContains(t, s.Validate(), `profile "BROKEN": nodesets must not be empty`)
}

func TestNormalizeLabel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"auto-select", "auto-select"},
		{"Auto-Select", "auto-select"},
		{"  @auto-select  ", "auto-select"},
		{"@AUTO-SELECT", "auto-select"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeLabel(tt.in))
		})
	}
}

func TestValidate_NormalizesLabels(t *testing.T) {
	// The "@" that marks a label in the profile input is tolerated in config too.
	p := validProfile()
	p.Labels = []string{"  @Auto-Select "}
	require.NoError(t, p.Validate())
	assert.Equal(t, []string{autoSelectLabel}, p.Labels)
	assert.True(t, p.HasLabel(autoSelectLabel))
}

func TestValidate_EmptyLabel(t *testing.T) {
	p := validProfile()
	p.Labels = []string{"  "}
	assert.ErrorContains(t, p.Validate(), "labels[0]: must not be empty")
}

func TestValidate_MalformedLabel(t *testing.T) {
	for _, label := range []string{
		"auto select",  // space
		"4-gpu",        // leading digit
		"-auto-select", // leading hyphen
		"auto/select",  // separator that is not . _ or -
	} {
		t.Run(label, func(t *testing.T) {
			p := validProfile()
			p.Labels = []string{label}
			assert.ErrorContains(t, p.Validate(), "must match")
		})
	}
}

func TestValidate_AcceptedLabelShapes(t *testing.T) {
	p := validProfile()
	p.Labels = []string{"auto-select", "auto-select-cpu", "gpu4", "a.b_c-d"}
	assert.NoError(t, p.Validate())
}

func TestValidate_DuplicateLabel(t *testing.T) {
	p := validProfile()
	p.Labels = []string{autoSelectLabel, "@AUTO-SELECT"}
	assert.ErrorContains(t, p.Validate(), "duplicate label")
}

func TestConfigCandidates(t *testing.T) {
	s := validConfig()
	// Unlabelled, so configured but not auto-selectable.
	s.Profiles["KCS_CPU"] = validProfile()
	// Labelled, and sorts before MAN_H100.
	labelled := validProfile()
	labelled.Labels = []string{autoSelectLabel}
	s.Profiles["KCS_B200"] = labelled

	require.NoError(t, s.Validate())
	assert.Equal(t, []string{"KCS_B200", "MAN_H100"}, s.Candidates(autoSelectLabel))
}

func TestConfigCandidates_NoneLabelled(t *testing.T) {
	// A config with no candidates is valid; only auto-selection fails on it, so
	// explicitly requested profiles keep working.
	s := validConfig()
	s.Profiles["MAN_H100"] = validProfile()
	require.NoError(t, s.Validate())
	assert.Empty(t, s.Candidates(autoSelectLabel))
}

func TestConfigValidate_DefaultsSettings(t *testing.T) {
	s := validConfig()
	require.NoError(t, s.Validate())
	assert.Equal(t, SchedulerSettings{
		RunsPerTick: defaultRunsPerTick,
		MaxInFlight: defaultMaxInFlight,
	}, s.Scheduler)
	assert.Equal(t, SelectorSettings{
		TimeoutMinutes: defaultTimeoutMinutes,
		PollSeconds:    defaultPollSeconds,
	}, s.Selector)
}

func TestConfigValidate_KeepsExplicitSettings(t *testing.T) {
	s := validConfig()
	s.Scheduler = SchedulerSettings{RunsPerTick: 3, MaxInFlight: 5}
	s.Selector.TimeoutMinutes = 30
	s.Selector.PollSeconds = 15
	require.NoError(t, s.Validate())
	assert.Equal(t, 3, s.Scheduler.RunsPerTick)
	assert.Equal(t, 30, s.Selector.TimeoutMinutes)
}

func TestConfigValidate_WritesProfileDefaultsBack(t *testing.T) {
	s := validConfig()
	require.NoError(t, s.Validate())
	assert.Equal(t, CapacityStrategyWarn, s.Profiles["MAN_H100"].CapacityStrategy)
}

func TestConfigNames(t *testing.T) {
	s := validConfig()
	s.Profiles["KCS_B200"] = validProfile()
	assert.Equal(t, []string{"KCS_B200", "MAN_H100"}, s.Names())
}
