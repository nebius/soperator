package acceptance

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
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
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
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
	t.Setenv("TEAMCITY_VERSION", "")
	for _, reservations := range []bool{false, true} {
		for _, phase := range []string{"clean", "before", "step", "after"} {
			t.Run(fmt.Sprintf("reservations=%t/%s", reservations, phase), func(t *testing.T) {
				cleanupErr := errors.New("restore skipped scenario")
				suite := reservationTestSuite("skip", func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
					var needsCleanup bool
					sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
						needsCleanup = strings.Contains(scenario.Steps[0].Text, `"yes"`)
						if phase == "before" {
							if needsCleanup {
								framework.RecordWorkerCleanupError(ctx, cleanupErr)
							}
							return ctx, godog.ErrSkip
						}
						return ctx, nil
					})
					run := func(ctx context.Context) error {
						if phase == "step" && needsCleanup {
							framework.RecordWorkerCleanupError(ctx, cleanupErr)
						}
						return godog.ErrSkip
					}
					sc.Step(`^the test runs with cleanup "(?:yes|no)"$`, run)
					sc.Step(`^the test runs$`, run)
					if phase == "after" {
						sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
							if needsCleanup {
								return ctx, cleanupErr
							}
							return ctx, nil
						})
					}
				})
				suite.Source.FS = fstest.MapFS{"test.feature": {Data: []byte(`Feature: cleanup
  Scenario Outline: test
    Given the test runs with cleanup "<cleanup>"
    Then the test runs
    Examples:
      | cleanup |
      | yes     |
      | no      |
`)}}
				suite.WorkerReservations = reservations
				suite.Concurrency = 2
				outputDir := t.TempDir()
				junitDir := outputDir
				if phase == "after" {
					t.Setenv("TEAMCITY_VERSION", "2026.1")
					junitDir = filepath.Join(outputDir, "junit")
				}
				runner, err := NewRunner(RunnerConfig{KubectlContext: "dev", TargetSoperatorVersion: "5.0.0", OutputDir: outputDir, Suites: []SuiteConfig{suite}})
				require.NoError(t, err)
				err = runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites)
				wantFailures := 0
				if phase == "clean" {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, cleanupErr)
					assert.Equal(t, 1, strings.Count(err.Error(), cleanupErr.Error()))
					wantFailures = 1
				}

				data, err := os.ReadFile(filepath.Join(outputDir, "skip.cucumber.json"))
				require.NoError(t, err)
				var cucumber []struct {
					Elements []struct {
						Steps []struct {
							Result struct {
								Status       string
								ErrorMessage string `json:"error_message"`
							}
						}
					}
				}
				require.NoError(t, json.Unmarshal(data, &cucumber))
				require.Len(t, cucumber, 1)
				require.Len(t, cucumber[0].Elements, 2)
				for row, element := range cucumber[0].Elements {
					steps := element.Steps
					require.Len(t, steps, 2)
					if phase == "clean" || row == 1 {
						for _, step := range steps {
							assert.Equal(t, "skipped", step.Result.Status)
						}
					} else {
						assert.Equal(t, "failed", steps[1].Result.Status)
						assert.Contains(t, steps[1].Result.ErrorMessage, cleanupErr.Error())
					}
				}

				data, err = os.ReadFile(filepath.Join(junitDir, "skip.junit.xml"))
				require.NoError(t, err)
				var junit struct {
					Failures int `xml:"failures,attr"`
					Suites   []struct {
						Failures int `xml:"failures,attr"`
						Cases    []struct {
							Status  string `xml:"status,attr"`
							Failure *struct {
								Message string `xml:"message,attr"`
							} `xml:"failure"`
						} `xml:"testcase"`
					} `xml:"testsuite"`
				}
				require.NoError(t, xml.Unmarshal(data, &junit))
				assert.Equal(t, wantFailures, junit.Failures)
				require.Len(t, junit.Suites, 1)
				assert.Equal(t, wantFailures, junit.Suites[0].Failures)
				require.Len(t, junit.Suites[0].Cases, 2)
				for row, result := range junit.Suites[0].Cases {
					if phase == "clean" || row == 1 {
						assert.Nil(t, result.Failure)
					} else {
						assert.Equal(t, "failed", result.Status)
						require.NotNil(t, result.Failure)
						assert.Contains(t, result.Failure.Message, cleanupErr.Error())
					}
				}
			})
		}
	}
}

func TestRunnerCleanupReportsHandleEmptyScenariosAndFeatures(t *testing.T) {
	t.Setenv("TEAMCITY_VERSION", "")
	cleanupErr := errors.New("restore skipped scenario")
	suite := reservationTestSuite("empty", func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
		sc.Step(`^the test runs$`, func() error { return nil })
		sc.Step(`^cleanup fails before skipping$`, func(ctx context.Context) error {
			framework.RecordWorkerCleanupError(ctx, cleanupErr)
			return godog.ErrSkip
		})
	})
	suite.WorkerReservations = false
	suite.Source = FeatureSource{
		FS: fstest.MapFS{
			"empty.feature": {Data: []byte("Feature: A feature without scenarios\n")},
			"test.feature": {Data: []byte(`Feature: cleanup
  Scenario: empty before
  Scenario: clean
    Given the test runs
  Scenario: cleanup failure
    Given cleanup fails before skipping
  Scenario: empty after
`)},
		},
		Paths: []string{"empty.feature", "test.feature"},
	}
	outputDir := t.TempDir()
	runner, err := NewRunner(RunnerConfig{KubectlContext: "dev", TargetSoperatorVersion: "5.0.0", OutputDir: outputDir, Suites: []SuiteConfig{suite}})
	require.NoError(t, err)
	require.ErrorIs(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites), cleanupErr)

	data, err := os.ReadFile(filepath.Join(outputDir, "empty.cucumber.json"))
	require.NoError(t, err)
	var cucumber []struct {
		Elements []struct {
			Steps []struct {
				Result struct{ Status string }
			}
		}
	}
	require.NoError(t, json.Unmarshal(data, &cucumber))
	require.Len(t, cucumber, 1)
	require.Len(t, cucumber[0].Elements, 4)
	assert.Empty(t, cucumber[0].Elements[0].Steps)
	assert.Empty(t, cucumber[0].Elements[3].Steps)
	for i, status := range map[int]string{1: "passed", 2: "failed"} {
		require.Len(t, cucumber[0].Elements[i].Steps, 1)
		assert.Equal(t, status, cucumber[0].Elements[i].Steps[0].Result.Status)
	}

	data, err = os.ReadFile(filepath.Join(outputDir, "empty.junit.xml"))
	require.NoError(t, err)
	var junit struct {
		Failures int `xml:"failures,attr"`
		Suites   []struct {
			Cases []struct {
				Status  string `xml:"status,attr"`
				Failure *struct {
					Message string `xml:"message,attr"`
				} `xml:"failure"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	require.NoError(t, xml.Unmarshal(data, &junit))
	assert.Equal(t, 1, junit.Failures)
	require.Len(t, junit.Suites, 1)
	require.Len(t, junit.Suites[0].Cases, 4)
	for i, result := range junit.Suites[0].Cases {
		if i == 2 {
			assert.Equal(t, "failed", result.Status)
			require.NotNil(t, result.Failure)
			assert.Contains(t, result.Failure.Message, cleanupErr.Error())
		} else {
			assert.Nil(t, result.Failure)
		}
	}
}

func TestConcurrentSuitesKeepSequentialScenariosUnleased(t *testing.T) {
	runtime := &reservationRunnerRuntime{}
	suite := reservationTestSuite("modes", func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, runtime framework.Runtime) {
		sc.Step(`^the sequential case runs$`, func(ctx context.Context) error {
			_, err := framework.NewWorkerSelector(framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime), "soperator").Acquire(ctx, framework.WorkerRequirements{Count: 1})
			require.ErrorContains(t, err, "ownership scope is required")
			return nil
		})
		sc.Step(`^the test runs$`, func(ctx context.Context) error {
			_, err := framework.NewWorkerSelector(framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime), "soperator").Acquire(ctx, framework.WorkerRequirements{Count: 1})
			return err
		})
	})
	suite.Source = FeatureSource{FS: fstest.MapFS{"test.feature": {Data: []byte("Feature: modes\n  @sequential\n  Scenario: sequential\n    Given the sequential case runs\n  Scenario: parallel\n    Given the test runs\n")}}, Paths: []string{"test.feature"}}
	runner, err := NewRunner(RunnerConfig{KubectlContext: "dev", TargetSoperatorVersion: "5.0.0", Suites: ConcurrentSuites(suite, 1)})
	require.NoError(t, err)
	require.NoError(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), runtime, runner.suites))
	assert.Equal(t, []string{"delete lease"}, runtime.events)
	assert.Empty(t, runtime.lease)
}

func TestRunnerRestoresAndFinalizesAfterCancellation(t *testing.T) {
	runtime := &reservationRunnerRuntime{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	suite := reservationTestSuite("cancel", func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, runtime framework.Runtime) {
		sc.Step(`^the test runs$`, func(ctx context.Context) error {
			_, err := framework.NewWorkerSelector(framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime), "soperator").Acquire(ctx, framework.WorkerRequirements{Count: 1})
			if err != nil {
				return err
			}
			cancel()
			return ctx.Err()
		})
		sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
			assert.ErrorIs(t, ctx.Err(), context.Canceled)
			runtime.(*reservationRunnerRuntime).events = append(runtime.(*reservationRunnerRuntime).events, "external recovery")
			_, err := framework.NewSlurmClient(runtime).SubmitBatch(context.WithoutCancel(ctx), framework.SbatchOptions{JobName: "recovery", Wrap: "true"})
			return ctx, err
		})
	})
	runner, err := NewRunner(RunnerConfig{KubectlContext: "dev", TargetSoperatorVersion: "5.0.0", Suites: []SuiteConfig{suite}})
	require.NoError(t, err)
	require.Error(t, runner.runConfiguredSuites(ctx, testClusterInfo(), runtime, runner.suites))
	assert.Equal(t, []string{"external recovery", "cancel owned job", "delete lease"}, runtime.events)
	assert.Empty(t, runtime.lease)
}

func TestRunnerRetainsLeaseWhenExternalCleanupReturnsFailure(t *testing.T) {
	for _, stepErr := range []error{nil, godog.ErrSkip, errors.New("original step failure"), context.Canceled} {
		t.Run(fmt.Sprint(stepErr), func(t *testing.T) {
			runtime := &reservationRunnerRuntime{}
			cleanupErr := errors.New("external recovery smoke failed")
			if stepErr == context.Canceled {
				cleanupErr = stepErr
			}
			suite := reservationTestSuite("unsafe", func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, runtime framework.Runtime) {
				sc.Step(`^the test runs$`, func(ctx context.Context) error {
					_, err := framework.NewWorkerSelector(framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime), "soperator").Acquire(ctx, framework.WorkerRequirements{Count: 1})
					return errors.Join(err, stepErr)
				})
				sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
					return ctx, cleanupErr
				})
			})
			runner, err := NewRunner(RunnerConfig{KubectlContext: "dev", TargetSoperatorVersion: "5.0.0", Suites: []SuiteConfig{suite}})
			require.NoError(t, err)
			require.ErrorIs(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), runtime, runner.suites), cleanupErr)
			assert.NotEmpty(t, runtime.lease)
			assert.NotContains(t, runtime.events, "delete lease")
		})
	}
}
func TestRunnerRejectsNegativeReservationTimeouts(t *testing.T) {
	for _, config := range []RunnerConfig{{WorkerAcquireTimeout: -time.Second}, {WorkerCleanupTimeout: -time.Second}} {
		_, err := NewRunner(config)
		require.ErrorContains(t, err, "must not be negative")
	}
}
