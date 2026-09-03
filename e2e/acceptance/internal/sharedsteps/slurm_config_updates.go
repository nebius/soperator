package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	slurmConfigStabilityWindow = 30 * time.Second
	slurmConfigUpdateTimeout   = 5 * time.Minute
)

type SlurmConfigUpdates struct {
	info    *framework.ClusterInfo
	runtime framework.Runtime
	slurm   *framework.SlurmClient
	kubectl *framework.KubectlClient

	originalCustomConfig *string
	originalJobRequeue   string
	workerStartTimes     map[string]time.Time
	stateRecorded        bool
	restoreNeeded        bool
}

func NewSlurmConfigUpdates(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient,
) *SlurmConfigUpdates {
	return &SlurmConfigUpdates{
		info:    info,
		runtime: runtime,
		slurm:   slurm,
		kubectl: kubectl,
	}
}

func (s *SlurmConfigUpdates) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the current custom Slurm configuration and worker start times are recorded$`, s.recordCurrentState)
	sc.Step(`^worker start times remain unchanged while the Slurm configuration is unchanged$`, s.checkWorkerStartTimesStable)
	sc.Step(`^JobRequeue is overridden to 0 in the SlurmCluster$`, s.overrideJobRequeue)
	sc.Step(`^the effective JobRequeue value becomes 0$`, s.checkOverriddenJobRequeue)
	sc.Step(`^every active worker has been reconfigured$`, s.checkWorkersReconfigured)
	sc.Step(`^the original custom Slurm configuration is restored$`, s.restoreOriginalCustomConfig)
	sc.Step(`^the effective JobRequeue value is restored$`, s.checkRestoredJobRequeue)
	sc.Step(`^every active worker has been reconfigured again$`, s.checkWorkersReconfiguredAgain)
}

func (s *SlurmConfigUpdates) CleanupAndReset(ctx context.Context) {
	if s.restoreNeeded && s.stateRecorded {
		cleanupCtx, cancel := context.WithTimeout(ctx, slurmConfigUpdateTimeout)
		defer cancel()

		if err := s.patchCustomConfig(cleanupCtx, s.originalCustomConfig); err != nil {
			s.runtime.Logf("cleanup: restore SlurmCluster custom config: %v", err)
		} else if err := s.waitForJobRequeue(cleanupCtx, s.originalJobRequeue); err != nil {
			s.runtime.Logf("cleanup: wait for original JobRequeue value: %v", err)
		}
	}

	s.originalCustomConfig = nil
	s.originalJobRequeue = ""
	s.workerStartTimes = nil
	s.stateRecorded = false
	s.restoreNeeded = false
}

func (s *SlurmConfigUpdates) recordCurrentState(ctx context.Context) error {
	cluster, err := s.kubectl.SlurmCluster(ctx, s.info.SlurmClusterName)
	if err != nil {
		return err
	}
	if cluster.CustomSlurmConfig != nil {
		original := *cluster.CustomSlurmConfig
		s.originalCustomConfig = &original
	}

	s.originalJobRequeue, err = s.effectiveSlurmSetting(ctx, "JobRequeue")
	if err != nil {
		return err
	}
	if s.originalJobRequeue == "0" {
		return fmt.Errorf("change JobRequeue from 0: scenario requires a different original value")
	}

	s.workerStartTimes, err = s.activeWorkerStartTimes(ctx)
	if err != nil {
		return err
	}
	s.stateRecorded = true
	return nil
}

func (s *SlurmConfigUpdates) checkWorkerStartTimesStable(ctx context.Context) error {
	if !s.stateRecorded {
		return fmt.Errorf("record current Slurm configuration before checking worker start times")
	}

	timer := time.NewTimer(slurmConfigStabilityWindow)
	defer timer.Stop()
	ticker := time.NewTicker(framework.DefaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-ticker.C:
			current, err := s.activeWorkerStartTimes(ctx)
			if err != nil {
				return err
			}
			if err := compareWorkerStartTimes(s.workerStartTimes, current, false); err != nil {
				return fmt.Errorf("detect worker reconfiguration while Slurm configuration was unchanged: %w", err)
			}
		}
	}
}

func (s *SlurmConfigUpdates) overrideJobRequeue(ctx context.Context) error {
	if !s.stateRecorded {
		return fmt.Errorf("record current Slurm configuration before applying an override")
	}

	override := customSlurmConfigWithJobRequeue(s.originalCustomConfig, "0")
	s.restoreNeeded = true
	return s.patchCustomConfig(ctx, &override)
}

func (s *SlurmConfigUpdates) checkOverriddenJobRequeue(ctx context.Context) error {
	return s.waitForJobRequeue(ctx, "0")
}

func (s *SlurmConfigUpdates) checkWorkersReconfigured(ctx context.Context) error {
	return s.waitForWorkersReconfigured(ctx)
}

func (s *SlurmConfigUpdates) restoreOriginalCustomConfig(ctx context.Context) error {
	if !s.restoreNeeded {
		return fmt.Errorf("restore original custom Slurm configuration: no override was applied")
	}
	return s.patchCustomConfig(ctx, s.originalCustomConfig)
}

func (s *SlurmConfigUpdates) checkRestoredJobRequeue(ctx context.Context) error {
	return s.waitForJobRequeue(ctx, s.originalJobRequeue)
}

func (s *SlurmConfigUpdates) checkWorkersReconfiguredAgain(ctx context.Context) error {
	if err := s.waitForWorkersReconfigured(ctx); err != nil {
		return err
	}
	s.restoreNeeded = false
	return nil
}

func (s *SlurmConfigUpdates) patchCustomConfig(ctx context.Context, value *string) error {
	if err := s.kubectl.PatchSlurmClusterCustomConfig(ctx, s.info.SlurmClusterName, value); err != nil {
		return err
	}
	return nil
}

func (s *SlurmConfigUpdates) effectiveSlurmSetting(ctx context.Context, setting string) (string, error) {
	output, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "scontrol show config")
	if err != nil {
		return "", fmt.Errorf("read effective Slurm configuration: %w", err)
	}
	configuration, err := parseSlurmConfiguration(output)
	if err != nil {
		return "", fmt.Errorf("parse effective Slurm configuration: %w", err)
	}
	value, found := configuration[setting]
	if !found {
		return "", fmt.Errorf("find %s in effective Slurm configuration", setting)
	}
	return value, nil
}

func (s *SlurmConfigUpdates) waitForJobRequeue(ctx context.Context, expected string) error {
	return s.runtime.WaitFor(ctx,
		fmt.Sprintf("effective JobRequeue value to become %s", expected),
		slurmConfigUpdateTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			actual, err := s.effectiveSlurmSetting(waitCtx, "JobRequeue")
			if err != nil {
				return false, err
			}
			if actual != expected {
				return false, fmt.Errorf("observe JobRequeue=%q, expected %q", actual, expected)
			}
			return true, nil
		},
	)
}

func (s *SlurmConfigUpdates) activeWorkerStartTimes(ctx context.Context) (map[string]time.Time, error) {
	names, err := s.slurm.MainPartitionNodeNames(ctx)
	if err != nil {
		return nil, err
	}

	startTimes := make(map[string]time.Time, len(names))
	for _, name := range names {
		node, err := s.slurm.NodeInfo(ctx, name)
		if err != nil {
			return nil, err
		}
		if node.HasStateFlag("NOT_RESPONDING") || node.HasStateFlag("POWERING_DOWN") || node.HasStateFlag("POWERED_DOWN") {
			continue
		}
		if node.SlurmdStartTime.IsZero() {
			return nil, fmt.Errorf("read valid SlurmdStartTime for active worker %s", name)
		}
		startTimes[name] = node.SlurmdStartTime
	}
	if len(startTimes) == 0 {
		return nil, fmt.Errorf("find active workers with SlurmdStartTime")
	}
	return startTimes, nil
}

func (s *SlurmConfigUpdates) waitForWorkersReconfigured(ctx context.Context) error {
	if len(s.workerStartTimes) == 0 {
		return fmt.Errorf("record worker start times before waiting for reconfiguration")
	}

	var updated map[string]time.Time
	err := s.runtime.WaitFor(ctx,
		"every active worker to be reconfigured",
		slurmConfigUpdateTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			current, err := s.activeWorkerStartTimes(waitCtx)
			if err != nil {
				return false, err
			}
			if err := compareWorkerStartTimes(s.workerStartTimes, current, true); err != nil {
				return false, err
			}
			updated = current
			return true, nil
		},
	)
	if err != nil {
		return err
	}
	s.workerStartTimes = updated
	return nil
}

func customSlurmConfigWithJobRequeue(original *string, value string) string {
	if original == nil || *original == "" {
		return "JobRequeue=" + value + "\n"
	}

	config := *original
	if !strings.HasSuffix(config, "\n") {
		config += "\n"
	}
	return config + "JobRequeue=" + value + "\n"
}

func compareWorkerStartTimes(expected, actual map[string]time.Time, requireAdvanced bool) error {
	for name, before := range expected {
		after, found := actual[name]
		if !found {
			return fmt.Errorf("find worker %s among active workers", name)
		}
		if requireAdvanced {
			if !after.After(before) {
				return fmt.Errorf("observe worker %s SlurmdStartTime still at %s", name, after.Format(time.RFC3339))
			}
			continue
		}
		if !after.Equal(before) {
			return fmt.Errorf("observe worker %s SlurmdStartTime change from %s to %s",
				name, before.Format(time.RFC3339), after.Format(time.RFC3339))
		}
	}
	return nil
}
