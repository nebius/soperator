package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gpuProfile(projectID, region, platform, fabric string, size int) Profile {
	return Profile{
		NebiusProjectID:  projectID,
		NebiusRegion:     region,
		NebiusTenantID:   "tenant-456",
		CapacityStrategy: CapacityStrategyWarn,
		NodeSets: []NodeSetDef{
			{
				Name:             "worker",
				Platform:         platform,
				Preset:           "8gpu-128vcpu-1600gb",
				Size:             size,
				InfinibandFabric: fabric,
			},
		},
	}
}

// testConfig mirrors the real config shape: two regions with one project each, plus a
// second profile sharing the us-central1 project (as KCS_B200 and KCS_CPU do). Only
// the two GPU profiles carry the auto-select label.
func testConfig() E2EConfig {
	kcs := gpuProfile("project-kcs", "us-central1", "gpu-b200-sxm", "us-central1-b", 2)
	kcs.Labels = []string{autoSelectLabel}

	mdc := gpuProfile("project-mdc", "me-west1", "gpu-b200-sxm-a", "me-west1-a", 2)
	mdc.Labels = []string{autoSelectLabel}

	return E2EConfig{
		Scheduler: SchedulerSettings{RunsPerTick: 1, MaxInFlight: 2},
		Selector:  SelectorSettings{TimeoutMinutes: 60, PollSeconds: 60},
		Profiles: map[string]Profile{
			"KCS_B200": kcs,
			"MDC_B200": mdc,
			"KCS_CPU": {
				NebiusProjectID:  "project-kcs",
				NebiusRegion:     "us-central1",
				NebiusTenantID:   "tenant-456",
				CapacityStrategy: CapacityStrategyWarn,
				NodeSets: []NodeSetDef{
					{Name: "worker", Platform: "cpu-d3", Preset: "32vcpu-128gb", Size: 2},
				},
			},
		},
	}
}

// label returns the cfg with an extra label on one of its profiles.
func withLabel(cfg E2EConfig, name, label string) E2EConfig {
	profile := cfg.Profiles[name]
	profile.Labels = append(profile.Labels, label)
	cfg.Profiles[name] = profile
	return cfg
}

func alwaysAvailable(Profile, map[affinityKey]uint64) (bool, error) { return true, nil }

func neverAvailable(Profile, map[affinityKey]uint64) (bool, error) { return false, nil }

func TestFilterFeasible_AllFree(t *testing.T) {
	cfg := testConfig()
	feasible, err := filterFeasible(cfg, cfg.Candidates(autoSelectLabel), nil, alwaysAvailable)
	require.NoError(t, err)
	assert.Equal(t, []string{"KCS_B200", "MDC_B200"}, feasible)
}

func TestFilterFeasible_SkipsClaimedProject(t *testing.T) {
	cfg := testConfig()
	claims := []RunClaim{{RunID: 1, ProfileName: "KCS_B200", ProjectID: "project-kcs"}}
	feasible, err := filterFeasible(cfg, cfg.Candidates(autoSelectLabel), claims, alwaysAvailable)
	require.NoError(t, err)
	assert.Equal(t, []string{"MDC_B200"}, feasible)
}

func TestFilterFeasible_SiblingProfileSharesTheClaimedProject(t *testing.T) {
	cfg := withLabel(testConfig(), "KCS_CPU", autoSelectLabel)

	// A run holding project-kcs through KCS_CPU also blocks KCS_B200: they share the
	// project, and therefore the concurrency group.
	claims := []RunClaim{{RunID: 7, ProfileName: "KCS_CPU", ProjectID: "project-kcs"}}
	feasible, err := filterFeasible(cfg, cfg.Candidates(autoSelectLabel), claims, alwaysAvailable)
	require.NoError(t, err)
	assert.Equal(t, []string{"MDC_B200"}, feasible)
}

func TestFilterFeasible_SkipsProfileWithoutCapacity(t *testing.T) {
	cfg := testConfig()
	feasible, err := filterFeasible(cfg, cfg.Candidates(autoSelectLabel), nil, neverAvailable)
	require.NoError(t, err)
	assert.Empty(t, feasible)
}

func TestFilterFeasible_IgnoresUnlabelledProfiles(t *testing.T) {
	// KCS_CPU is configured but unlabelled, so it is never returned.
	cfg := testConfig()
	feasible, err := filterFeasible(cfg, cfg.Candidates(autoSelectLabel), nil, alwaysAvailable)
	require.NoError(t, err)
	assert.NotContains(t, feasible, "KCS_CPU")
}

func TestFilterFeasible_PassesReservedDemandToOracle(t *testing.T) {
	var seen map[affinityKey]uint64
	oracle := func(_ Profile, reserved map[affinityKey]uint64) (bool, error) {
		seen = reserved
		return true, nil
	}

	cfg := testConfig()
	claims := []RunClaim{{RunID: 1, ProfileName: "KCS_B200", ProjectID: "project-kcs"}}
	_, err := filterFeasible(cfg, cfg.Candidates(autoSelectLabel), claims, oracle)
	require.NoError(t, err)

	assert.Equal(t, uint64(16), seen[affinityKey{Platform: "gpu-b200-sxm", Fabric: "us-central1-b"}])
}

func TestFilterFeasible_OracleError(t *testing.T) {
	boom := errors.New("capacity api down")
	cfg := testConfig()
	_, err := filterFeasible(cfg, cfg.Candidates(autoSelectLabel), nil, func(Profile, map[affinityKey]uint64) (bool, error) {
		return false, boom
	})
	assert.ErrorIs(t, err, boom)
}

func TestReservedDemand(t *testing.T) {
	cfg := testConfig()
	claims := []RunClaim{
		{RunID: 1, ProfileName: "KCS_B200", ProjectID: "project-kcs"},
		{RunID: 2, ProfileName: "MDC_B200", ProjectID: "project-mdc"},
		{RunID: 3, ProfileName: "KCS_CPU", ProjectID: "project-kcs"},
		{RunID: 4, ProfileName: "GONE_FROM_CONFIG", ProjectID: "project-old"},
	}

	reserved := reservedDemand(cfg, claims)

	assert.Equal(t, uint64(16), reserved[affinityKey{Platform: "gpu-b200-sxm", Fabric: "us-central1-b"}])
	assert.Equal(t, uint64(16), reserved[affinityKey{Platform: "gpu-b200-sxm-a", Fabric: "me-west1-a"}])
	// CPU-only and unknown profiles contribute nothing.
	assert.Len(t, reserved, 2)
}

func TestReservedDemand_SumsRunsOnTheSameAffinity(t *testing.T) {
	cfg := testConfig()
	claims := []RunClaim{
		{RunID: 1, ProfileName: "KCS_B200", ProjectID: "project-kcs"},
		{RunID: 2, ProfileName: "KCS_B200", ProjectID: "project-kcs"},
	}

	reserved := reservedDemand(cfg, claims)
	assert.Equal(t, uint64(32), reserved[affinityKey{Platform: "gpu-b200-sxm", Fabric: "us-central1-b"}])
}

func noClaims(context.Context) ([]RunClaim, error) { return nil, nil }

func testOptions(oracle capacityOracle) selectOptions {
	return selectOptions{
		timeout: time.Second,
		poll:    time.Millisecond,
		claims:  noClaims,
		oracle:  oracle,
		pick:    func(int) int { return 0 },
	}
}

func TestSelectProfile_PicksAvailableProfile(t *testing.T) {
	name, profile, err := selectProfile(context.Background(), testConfig(), autoSelectLabel, testOptions(alwaysAvailable))
	require.NoError(t, err)
	assert.Equal(t, "KCS_B200", name)
	assert.Equal(t, "project-kcs", profile.NebiusProjectID)
}

func TestSelectProfile_PickIsRandomOverFeasibleOnly(t *testing.T) {
	cfg := testConfig()
	claims := func(context.Context) ([]RunClaim, error) {
		return []RunClaim{{RunID: 1, ProfileName: "KCS_B200", ProjectID: "project-kcs"}}, nil
	}

	opts := testOptions(alwaysAvailable)
	opts.claims = claims
	// The only feasible candidate is at index 0 of the filtered list, not of the
	// full candidate list.
	opts.pick = func(n int) int {
		assert.Equal(t, 1, n)
		return 0
	}

	name, _, err := selectProfile(context.Background(), cfg, autoSelectLabel, opts)
	require.NoError(t, err)
	assert.Equal(t, "MDC_B200", name)
}

func TestSelectProfile_RetriesUntilCapacityFrees(t *testing.T) {
	attempts := 0
	opts := testOptions(func(Profile, map[affinityKey]uint64) (bool, error) {
		attempts++
		return attempts >= 3, nil
	})

	name, _, err := selectProfile(context.Background(), testConfig(), autoSelectLabel, opts)
	require.NoError(t, err)
	assert.Equal(t, "KCS_B200", name)
	assert.GreaterOrEqual(t, attempts, 3)
}

func TestSelectProfile_TimesOut(t *testing.T) {
	opts := testOptions(neverAvailable)
	opts.timeout = 20 * time.Millisecond

	_, _, err := selectProfile(context.Background(), testConfig(), autoSelectLabel, opts)
	assert.ErrorIs(t, err, ErrNoProfileAvailable)
}

func TestSelectProfile_NoLabelledProfiles(t *testing.T) {
	cfg := testConfig()
	for name, profile := range cfg.Profiles {
		profile.Labels = nil
		cfg.Profiles[name] = profile
	}

	_, _, err := selectProfile(context.Background(), cfg, autoSelectLabel, testOptions(alwaysAvailable))
	assert.ErrorContains(t, err, `no profile carries the label "auto-select"`)
}

func TestSelectProfile_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := testOptions(neverAvailable)
	_, _, err := selectProfile(ctx, testConfig(), autoSelectLabel, opts)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSelectProfile_ClaimsError(t *testing.T) {
	boom := errors.New("gh api failed")
	opts := testOptions(alwaysAvailable)
	opts.claims = func(context.Context) ([]RunClaim, error) { return nil, boom }

	_, _, err := selectProfile(context.Background(), testConfig(), autoSelectLabel, opts)
	assert.ErrorIs(t, err, boom)
}
