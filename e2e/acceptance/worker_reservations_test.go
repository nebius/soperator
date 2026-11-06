package acceptance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type reservationRunnerRuntime struct {
	framework.Runtime
	events []string
}

func (*reservationRunnerRuntime) Controller() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		if strings.HasPrefix(command, "sinfo ") {
			return "worker-0", nil
		}
		return "NodeName=worker-0 State=IDLE", nil
	})
}

func (*reservationRunnerRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		switch strings.Join(args[:2], " ") {
		case "get nodesets":
			return `{"items":[{"metadata":{"name":"worker"},"spec":{"replicas":1}}]}`, nil
		case "get pods":
			return `{"items":[{"metadata":{"name":"worker-0"},"spec":{"hostname":"worker-0","nodeName":"host-0"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`, nil
		case "get nodes":
			return `{"items":[{"metadata":{"name":"host-0"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`, nil
		default:
			return "", fmt.Errorf("unexpected kubectl arguments %v", args)
		}
	})
}

func (r *reservationRunnerRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		switch {
		case strings.HasPrefix(command, "scancel "):
			r.events = append(r.events, "cancel owned job")
		case strings.Contains(command, "sbatch "):
			return "123", nil
		}
		return "", nil
	})
}

func (*reservationRunnerRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	_, err := condition(ctx)
	return err
}

func (*reservationRunnerRuntime) Logf(string, ...any) {}

func reservationTestSuite(name string, register StepRegistrar) SuiteConfig {
	return SuiteConfig{
		Name: name, WorkerReservations: true,
		Source: FeatureSource{
			FS:    fstest.MapFS{"test.feature": {Data: []byte("Feature: reservations\n  Scenario: test\n    Given the test runs\n")}},
			Paths: []string{"test.feature"},
		},
		StepRegistrars: []StepRegistrar{register},
	}
}

func TestRunnerWorkerReservationsAreOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			runtime := &reservationRunnerRuntime{}
			suite := reservationTestSuite("opt-in", func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, runtime framework.Runtime) {
				sc.Step(`^the test runs$`, func(ctx context.Context) error {
					selector := framework.NewWorkerSelector(framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime), "soperator")
					_, err := selector.Acquire(ctx, framework.WorkerRequirements{Count: 1})
					if enabled {
						return err
					}
					require.ErrorContains(t, err, "ownership scope is required")
					_, err = framework.NewSlurmClient(runtime).SubmitBatch(ctx, framework.SbatchOptions{JobName: "legacy", Wrap: "true"})
					return err
				})
			})
			suite.WorkerReservations = enabled
			runner, err := NewRunner(RunnerConfig{
				KubectlContext: "dev", TargetSoperatorVersion: "5.0.0", Suites: []SuiteConfig{suite},
			})
			require.NoError(t, err)
			assert.Equal(t, enabled, runner.suites[0].WorkerReservations)
			require.NoError(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), runtime, runner.suites))
			assert.Empty(t, runtime.events)
		})
	}
	assert.False(t, SoperatorSuite("5.0.0").WorkerReservations)
}

func TestRunnerFinalizesAfterExternalCleanupAndKeepsQuarantineAcrossSuites(t *testing.T) {
	runtime := &reservationRunnerRuntime{}
	cleanupErr := errors.New("restore test fault")
	first := reservationTestSuite("first", func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, runtime framework.Runtime) {
		sc.Step(`^the test runs$`, func(ctx context.Context) error {
			selector := framework.NewWorkerSelector(framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime), "soperator")
			if _, err := selector.Acquire(ctx, framework.WorkerRequirements{Count: 1}); err != nil {
				return err
			}
			_, err := framework.NewSlurmClient(runtime).SubmitBatch(ctx, framework.SbatchOptions{
				JobName: "owned", Wrap: "true", Nodelist: []string{"worker-0"},
			})
			return err
		})
		sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
			runtime.(*reservationRunnerRuntime).events = append(runtime.(*reservationRunnerRuntime).events, "external cleanup")
			framework.RecordWorkerCleanupError(ctx, cleanupErr)
			return ctx, nil
		})
	})
	second := reservationTestSuite("second", func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, runtime framework.Runtime) {
		sc.Step(`^the test runs$`, func(ctx context.Context) error {
			selector := framework.NewWorkerSelector(framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime), "soperator")
			_, err := selector.Acquire(ctx, framework.WorkerRequirements{Count: 1})
			require.ErrorContains(t, err, "quarantined=restore test fault")
			return nil
		})
	})
	runner, err := NewRunner(RunnerConfig{
		KubectlContext: "dev", TargetSoperatorVersion: "5.0.0", Suites: []SuiteConfig{first, second},
	})
	require.NoError(t, err)
	err = runner.runConfiguredSuites(t.Context(), testClusterInfo(), runtime, runner.suites)
	require.ErrorIs(t, err, cleanupErr)
	assert.Equal(t, []string{"external cleanup", "cancel owned job"}, runtime.events)
}

func TestRunnerCleanupFailureDoesNotDisappearBehindSkip(t *testing.T) {
	for _, hookSkip := range []bool{false, true} {
		t.Run(fmt.Sprint(hookSkip), func(t *testing.T) {
			cleanupErr := errors.New("restore skipped scenario")
			suite := reservationTestSuite("skip", func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
				if hookSkip {
					sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
						framework.RecordWorkerCleanupError(ctx, cleanupErr)
						return ctx, godog.ErrSkip
					})
				}
				sc.Step(`^the test runs$`, func(ctx context.Context) error {
					framework.RecordWorkerCleanupError(ctx, cleanupErr)
					return godog.ErrSkip
				})
			})
			runner, err := NewRunner(RunnerConfig{
				KubectlContext: "dev", TargetSoperatorVersion: "5.0.0", Suites: []SuiteConfig{suite},
			})
			require.NoError(t, err)
			require.ErrorIs(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites), cleanupErr)
		})
	}
}

func TestRunnerRejectsNegativeReservationTimeouts(t *testing.T) {
	for _, config := range []RunnerConfig{
		{WorkerAcquireTimeout: -time.Second}, {WorkerCleanupTimeout: -time.Second},
	} {
		_, err := NewRunner(config)
		require.ErrorContains(t, err, "timeouts must not be negative")
	}
}
