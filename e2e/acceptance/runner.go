package acceptance

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/soperator-e2e/acceptance/framework"
	"nebius.ai/soperator-e2e/acceptance/internal/discovery"
	"nebius.ai/soperator-e2e/acceptance/internal/reports"
	"nebius.ai/soperator-e2e/versionfilter"
)

type timingCtxKey string

const (
	scenarioStartTimeKey    timingCtxKey = "acceptance_scenario_start_time"
	stepStartTimeKey        timingCtxKey = "acceptance_step_start_time"
	defaultSlurmClusterName              = "soperator"
)

var suiteNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Runner executes a configured Godog acceptance suite against a Soperator cluster.
type Runner struct {
	state          *framework.ClusterState
	suites         []SuiteConfig
	kubectlContext string
	reportDir      string
	discoveryHooks []DiscoveryHook
}

// DiscoveryHook discovers external runner-specific cluster data after base Soperator discovery.
type DiscoveryHook func(context.Context, *framework.ClusterState, framework.Exec) error

// Options configures an acceptance suite run.
type Options struct {
	KubectlContext         string
	SlurmClusterName       string
	TargetSoperatorVersion string
	ReportDir              string
	Suites                 []SuiteConfig
	DiscoveryHooks         []DiscoveryHook
	State                  *framework.ClusterState
}

// RegisterDefaultHooks registers common timing and skip hooks.
func RegisterDefaultHooks(sc *godog.ScenarioContext) {
	registerTimingHooks(sc)
	registerSkipHook(sc)
}

// NewRunner constructs an acceptance runner.
func NewRunner(opts Options) (*Runner, error) {
	kubectlContext := strings.TrimSpace(opts.KubectlContext)
	if kubectlContext == "" {
		return nil, fmt.Errorf("kubectl context is required")
	}

	state := opts.State
	if state == nil {
		state = &framework.ClusterState{}
	}

	slurmClusterName := strings.TrimSpace(opts.SlurmClusterName)
	if state.SlurmClusterName != "" {
		if slurmClusterName != "" && slurmClusterName != state.SlurmClusterName {
			return nil, fmt.Errorf("slurm cluster name mismatch: options=%q state=%q", slurmClusterName, state.SlurmClusterName)
		}
		slurmClusterName = state.SlurmClusterName
	}
	if slurmClusterName == "" {
		slurmClusterName = defaultSlurmClusterName
	}
	state.SlurmClusterName = slurmClusterName

	if state.WorkersByNodeSet == nil {
		state.WorkersByNodeSet = make(map[string][]framework.WorkerRef)
	}
	targetVersion := strings.TrimSpace(opts.TargetSoperatorVersion)
	if targetVersion == "" {
		return nil, fmt.Errorf("target Soperator version is required")
	}
	normalizedTargetVersion, err := framework.NormalizeSoperatorVersion(targetVersion)
	if err != nil {
		return nil, fmt.Errorf("target Soperator version: %w", err)
	}
	state.TargetSoperatorVersion = normalizedTargetVersion
	suites, err := normalizeSuites(opts.Suites)
	if err != nil {
		return nil, err
	}

	return &Runner{
		state:          state,
		suites:         suites,
		kubectlContext: kubectlContext,
		reportDir:      strings.TrimSpace(opts.ReportDir),
		discoveryHooks: slices.Clone(opts.DiscoveryHooks),
	}, nil
}

// Run executes the configured acceptance suite.
func (r *Runner) Run(ctx context.Context) error {
	w := newWorld(r.state, r.kubectlContext)
	if err := discovery.DiscoverCluster(ctx, w, r.state); err != nil {
		return fmt.Errorf("discover cluster before suite: %w", err)
	}
	if err := r.runDiscoveryHooks(ctx, w); err != nil {
		return err
	}

	return r.runConfiguredSuites(ctx, w)
}

func (r *Runner) runConfiguredSuites(ctx context.Context, exec framework.Exec) error {
	selectedSuites := 0
	var failures []error
	for _, suite := range r.suites {
		features, err := r.suiteFeaturePaths(suite)
		if err != nil {
			failures = append(failures, fmt.Errorf("select suite %q scenarios: %w", suite.Name, err))
			continue
		}
		if len(features) == 0 {
			log.Printf("acceptance: suite %s selected no scenarios", suite.Name)
			continue
		}
		selectedSuites++
		if err := r.runSuite(ctx, exec, suite, features); err != nil {
			failures = append(failures, err)
		}
	}
	if selectedSuites == 0 && len(failures) == 0 {
		failures = append(failures, fmt.Errorf("no acceptance scenarios compatible with Soperator version %s", r.state.TargetSoperatorVersion))
	}

	return errors.Join(failures...)
}

func (r *Runner) runDiscoveryHooks(ctx context.Context, exec framework.Exec) error {
	for i, hook := range r.discoveryHooks {
		if hook == nil {
			return fmt.Errorf("discovery hook %d is nil", i)
		}
		if err := hook(ctx, r.state, exec); err != nil {
			return fmt.Errorf("run discovery hook %d: %w", i, err)
		}
	}
	return nil
}

func (r *Runner) suiteTagFilter(suite SuiteConfig) string {
	var filters []string

	if suite.Tags != "" {
		filters = append(filters, suite.Tags)
	}
	if suite.FilterOptions.ExcludeUnstable {
		log.Printf("acceptance: suite %s excluding @unstable scenarios", suite.Name)
		filters = append(filters, "~@unstable")
	}
	if suite.FilterOptions.ExcludeMissingWorkerKinds && !r.state.HasGPUWorkers() {
		log.Printf("acceptance: suite %s found no GPU workers, excluding @gpu scenarios", suite.Name)
		filters = append(filters, "~@gpu")
	}
	if suite.FilterOptions.ExcludeMissingWorkerKinds && !r.state.HasCPUWorkers() {
		log.Printf("acceptance: suite %s found no CPU workers, excluding @cpu scenarios", suite.Name)
		filters = append(filters, "~@cpu")
	}

	return strings.Join(filters, " && ")
}

func normalizeSuites(suites []SuiteConfig) ([]SuiteConfig, error) {
	if len(suites) == 0 {
		return nil, fmt.Errorf("at least one suite is required")
	}

	out := make([]SuiteConfig, 0, len(suites))
	seen := make(map[string]struct{}, len(suites))
	for _, suite := range suites {
		name := strings.TrimSpace(suite.Name)
		if name == "" {
			return nil, fmt.Errorf("suite name is required")
		}
		if !suiteNamePattern.MatchString(name) {
			return nil, fmt.Errorf("suite name %q must match %s", name, suiteNamePattern.String())
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate suite name %q", name)
		}
		seen[name] = struct{}{}

		if suite.Source.FS == nil {
			return nil, fmt.Errorf("suite %q source FS is required", name)
		}
		paths := slices.Clone(suite.Source.Paths)
		if len(paths) == 0 {
			return nil, fmt.Errorf("suite %q must include at least one feature path", name)
		}
		for i, path := range paths {
			paths[i] = strings.TrimSpace(path)
			if paths[i] == "" {
				return nil, fmt.Errorf("suite %q feature path cannot be empty", name)
			}
		}
		registrars := slices.Clone(suite.StepRegistrars)
		for i, registrar := range registrars {
			if registrar == nil {
				return nil, fmt.Errorf("suite %q step registrar %d is nil", name, i)
			}
		}

		out = append(out, SuiteConfig{
			Name: name,
			Source: FeatureSource{
				FS:    suite.Source.FS,
				Paths: paths,
			},
			VersionAxes:    slices.Clone(suite.VersionAxes),
			Tags:           strings.TrimSpace(suite.Tags),
			StepRegistrars: registrars,
			FilterOptions:  suite.FilterOptions,
		})
	}
	return out, nil
}

func (r *Runner) suiteFeaturePaths(suite SuiteConfig) ([]string, error) {
	paths := slices.Clone(suite.Source.Paths)
	if len(suite.VersionAxes) > 0 {
		var err error
		paths, err = versionfilter.SelectScenarios(
			versionfilter.FeatureSource{FS: suite.Source.FS, Paths: suite.Source.Paths},
			suite.VersionAxes...,
		)
		if err != nil {
			return nil, err
		}
	}
	log.Printf("acceptance: target Soperator version=%s", r.state.TargetSoperatorVersion)
	log.Printf("acceptance: suite %s running scenarios: %s", suite.Name, strings.Join(paths, ", "))
	return paths, nil
}

func (r *Runner) runSuite(ctx context.Context, exec framework.Exec, suite SuiteConfig, features []string) error {
	format, err := reports.Format(r.reportDir, suite.Name)
	if err != nil {
		return fmt.Errorf("suite %q report format: %w", suite.Name, err)
	}

	tags := r.suiteTagFilter(suite)
	godogSuite := godog.TestSuite{
		Name: suite.Name,
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			r.initializeSuiteScenario(sc, exec, suite)
		},
		Options: &godog.Options{
			Format:         format,
			FS:             suite.Source.FS,
			Paths:          features,
			TestingT:       nil,
			Strict:         true,
			DefaultContext: ctx,
			Tags:           tags,
		},
	}

	suiteStart := time.Now()
	status := godogSuite.Run()
	log.Printf("acceptance: suite %s finished duration=%s", suite.Name, time.Since(suiteStart).Round(time.Millisecond))
	if status != 0 {
		return fmt.Errorf("suite %q: godog exited with status %d", suite.Name, status)
	}
	return nil
}

func (r *Runner) initializeSuiteScenario(sc *godog.ScenarioContext, exec framework.Exec, suite SuiteConfig) {
	RegisterDefaultHooks(sc)
	for _, register := range suite.StepRegistrars {
		register(sc, r.state, exec)
	}
}

func registerSkipHook(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		for _, t := range scenario.Tags {
			if t.Name == "@skip" {
				log.Printf("acceptance: scenario %q has @skip, marking as skipped", scenario.Name)
				return ctx, godog.ErrSkip
			}
		}
		return ctx, nil
	})
}

func registerTimingHooks(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		log.Printf("acceptance: scenario started: %q", scenario.Name)
		return context.WithValue(ctx, scenarioStartTimeKey, time.Now()), nil
	})

	sc.StepContext().Before(func(ctx context.Context, step *godog.Step) (context.Context, error) {
		return context.WithValue(ctx, stepStartTimeKey, time.Now()), nil
	})

	sc.StepContext().After(func(ctx context.Context, step *godog.Step, status godog.StepResultStatus, err error) (context.Context, error) {
		duration := "unknown"
		if startedAt, ok := ctx.Value(stepStartTimeKey).(time.Time); ok && !startedAt.IsZero() {
			duration = time.Since(startedAt).Round(time.Millisecond).String()
		}
		if err != nil {
			log.Printf("acceptance: step finished: %q status=%s duration=%s err=%v", step.Text, status, duration, err)
			return ctx, nil
		}
		log.Printf("acceptance: step finished: %q status=%s duration=%s", step.Text, status, duration)
		return ctx, nil
	})

	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		duration := "unknown"
		if startedAt, ok := ctx.Value(scenarioStartTimeKey).(time.Time); ok && !startedAt.IsZero() {
			duration = time.Since(startedAt).Round(time.Millisecond).String()
		}
		if err != nil {
			log.Printf("acceptance: scenario finished: %q duration=%s err=%v", scenario.Name, duration, err)
			return ctx, nil
		}
		log.Printf("acceptance: scenario finished: %q duration=%s", scenario.Name, duration)
		return ctx, nil
	})
}

func newWorld(state *framework.ClusterState, kubectlContext string) *world {
	return &world{
		logPrefix:      "acceptance",
		state:          state,
		kubectlContext: kubectlContext,
	}
}

func (w *world) logf(format string, args ...any) {
	log.Printf("%s: %s", w.logPrefix, fmt.Sprintf(format, args...))
}
