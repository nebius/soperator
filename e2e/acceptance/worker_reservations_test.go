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
	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reservationRunnerRuntime struct {
	framework.Runtime
	events []string
	lease  string
	jobs   bool
}

func (r *reservationRunnerRuntime) Controller() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		switch {
		case strings.HasPrefix(command, "sinfo "):
			return "worker-0", nil
		case command == "scontrol -o show nodes", strings.HasPrefix(command, "scontrol -o show node "):
			return "NodeName=worker-0 State=IDLE CPUAlloc=0", nil
		case strings.HasPrefix(command, "scontrol create reservation"):
			for _, part := range strings.Fields(command) {
				if name, ok := strings.CutPrefix(part, "ReservationName="); ok {
					r.lease = strings.Trim(name, "'")
				}
			}
			return "created", nil
		case strings.HasPrefix(command, "scontrol show hostnames "):
			return "worker-0", nil
		case strings.HasPrefix(command, "scontrol -o show reservation"):
			if r.lease == "" {
				if command == "scontrol -o show reservations" {
					return "No reservations in the system", nil
				}
				return "", errors.New("Invalid reservation name")
			}
			return "ReservationName=" + r.lease + " Nodes=worker-0 Flags=SPEC_NODES,STATIC Users=root Duration=08:00:00 State=ACTIVE", nil
		case strings.HasPrefix(command, "scancel "):
			r.events = append(r.events, "cancel owned job")
			r.jobs = false
			return "", nil
		case strings.HasPrefix(command, "squeue "):
			if r.jobs {
				return "123", nil
			}
			return "", nil
		case strings.HasPrefix(command, "scontrol delete "):
			r.events = append(r.events, "delete lease")
			r.lease = ""
			return "", nil
		}
		return "", fmt.Errorf("unexpected controller command %s", command)
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
		}
		return "", fmt.Errorf("unexpected kubectl arguments %v", args)
	})
}
func (r *reservationRunnerRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		if strings.Contains(command, "sbatch") {
			r.jobs = true
			return "123\n", nil
		}
		return "", nil
	})
}
func (*reservationRunnerRuntime) Logf(string, ...any) {}

func reservationTestSuite(name string, register StepRegistrar) SuiteConfig {
	return SuiteConfig{Name: name, WorkerReservations: true, Source: FeatureSource{FS: fstest.MapFS{"test.feature": {Data: []byte("Feature: reservations\n Scenario: test\n  Given the test runs\n")}}, Paths: []string{"test.feature"}}, StepRegistrars: []StepRegistrar{register}}
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
					return nil
				})
			})
			suite.WorkerReservations = enabled
			runner, err := NewRunner(RunnerConfig{KubectlContext: "dev", TargetSoperatorVersion: "5.0.0", Suites: []SuiteConfig{suite}})
			require.NoError(t, err)
			require.NoError(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), runtime, runner.suites))
			assert.Empty(t, runtime.lease)
		})
	}
	assert.False(t, SoperatorSuite("5.0.0").WorkerReservations)
}
func TestRunnerFinalizesAfterExternalRecoverySmoke(t *testing.T) {
	runtime := &reservationRunnerRuntime{}
	suite := reservationTestSuite("ordering", func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, runtime framework.Runtime) {
		sc.Step(`^the test runs$`, func(ctx context.Context) error {
			_, err := framework.NewWorkerSelector(framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime), "soperator").Acquire(ctx, framework.WorkerRequirements{Count: 1})
			return err
		})
		sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
			runtime.(*reservationRunnerRuntime).events = append(runtime.(*reservationRunnerRuntime).events, "external recovery")
			_, err := framework.NewSlurmClient(runtime).SubmitBatch(ctx, framework.SbatchOptions{JobName: "recovery", Wrap: "true"})
			return ctx, err
		})
	})
	runner, err := NewRunner(RunnerConfig{KubectlContext: "dev", TargetSoperatorVersion: "5.0.0", Suites: []SuiteConfig{suite}})
	require.NoError(t, err)
	require.NoError(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), runtime, runner.suites))
	assert.Equal(t, []string{"external recovery", "cancel owned job", "delete lease"}, runtime.events)
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
			runner, err := NewRunner(RunnerConfig{KubectlContext: "dev", TargetSoperatorVersion: "5.0.0", Suites: []SuiteConfig{suite}})
			require.NoError(t, err)
			require.ErrorIs(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites), cleanupErr)
		})
	}
}
func TestRunnerRejectsNegativeReservationTimeouts(t *testing.T) {
	for _, config := range []RunnerConfig{{WorkerAcquireTimeout: -time.Second}, {WorkerCleanupTimeout: -time.Second}} {
		_, err := NewRunner(config)
		require.ErrorContains(t, err, "must not be negative")
	}
}
