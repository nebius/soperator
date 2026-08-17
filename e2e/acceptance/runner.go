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
	"nebius.ai/soperator-e2e/acceptance/internal/reports"
	"nebius.ai/soperator-e2e/acceptance/internal/versionfilter"
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
	suites                 []SuiteConfig
	kubectlContext         string
	slurmClusterName       string
	targetSoperatorVersion string
	reportDir              string
}

// RunnerConfig configures an acceptance suite run.
type RunnerConfig struct {
	KubectlContext         string
	SlurmClusterName       string
	TargetSoperatorVersion string
	ReportDir              string
	Suites                 []SuiteConfig
}

// NewRunner constructs an acceptance runner.
func NewRunner(config RunnerConfig) (*Runner, error) {
	kubectlContext := strings.TrimSpace(config.KubectlContext)
	if kubectlContext == "" {
		return nil, fmt.Errorf("kubectl context is required")
	}

	slurmClusterName := strings.TrimSpace(config.SlurmClusterName)
	if slurmClusterName == "" {
		slurmClusterName = defaultSlurmClusterName
	}

	targetVersion := strings.TrimSpace(config.TargetSoperatorVersion)
	if targetVersion == "" {
		return nil, fmt.Errorf("target Soperator version is required")
	}
	normalizedTargetVersion, err := framework.NormalizeSoperatorVersion(targetVersion)
	if err != nil {
		return nil, fmt.Errorf("target Soperator version: %w", err)
	}
	suites, err := normalizeSuites(config.Suites)
	if err != nil {
		return nil, err
	}
	if len(suites) == 0 {
		return nil, fmt.Errorf("at least one suite is required")
	}

	return &Runner{
		suites:                 suites,
		kubectlContext:         kubectlContext,
		slurmClusterName:       slurmClusterName,
		targetSoperatorVersion: normalizedTargetVersion,
		reportDir:              strings.TrimSpace(config.ReportDir),
	}, nil
}

// Run executes the configured acceptance suite.
func (r *Runner) Run(ctx context.Context) error {
	w := newWorld(r.kubectlContext, r.slurmClusterName, r.targetSoperatorVersion)
	info := &framework.ClusterInfo{
		SlurmClusterName:       r.slurmClusterName,
		TargetSoperatorVersion: r.targetSoperatorVersion,
	}

	return r.runConfiguredSuites(ctx, info, w, r.suites)
}

func (r *Runner) runConfiguredSuites(ctx context.Context, info *framework.ClusterInfo, runtime framework.Runtime, suites []SuiteConfig) error {
	selectedSuites := 0
	var failures []error
	for _, suite := range suites {
		features, err := r.suiteFeaturePaths(info, suite)
		if err != nil {
			failures = append(failures, fmt.Errorf("select suite %q scenarios: %w", suite.Name, err))
			continue
		}
		if len(features) == 0 {
			log.Printf("acceptance: suite %s selected no scenarios", suite.Name)
			continue
		}
		selectedSuites++
		if err := r.runSuite(ctx, info, runtime, suite, features); err != nil {
			failures = append(failures, err)
		}
	}
	if selectedSuites == 0 && len(failures) == 0 {
		failures = append(failures, fmt.Errorf("no acceptance scenarios compatible with Soperator version %s", info.TargetSoperatorVersion))
	}

	return errors.Join(failures...)
}

func (r *Runner) suiteTagFilter(suite SuiteConfig) string {
	var filters []string

	if suite.Tags != "" {
		filters = append(filters, suite.Tags)
	}
	if suite.ExcludeUnstable {
		log.Printf("acceptance: suite %s excluding @unstable scenarios", suite.Name)
		filters = append(filters, "~@unstable")
	}

	return strings.Join(filters, " && ")
}

func normalizeSuites(suites []SuiteConfig) ([]SuiteConfig, error) {
	if len(suites) == 0 {
		return nil, nil
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
			VersionAxes:     slices.Clone(suite.VersionAxes),
			Tags:            strings.TrimSpace(suite.Tags),
			StepRegistrars:  registrars,
			ExcludeUnstable: suite.ExcludeUnstable,
		})
	}
	return out, nil
}

func (r *Runner) suiteFeaturePaths(info *framework.ClusterInfo, suite SuiteConfig) ([]string, error) {
	paths := slices.Clone(suite.Source.Paths)
	if len(suite.VersionAxes) > 0 {
		var err error
		axes := make([]versionfilter.Axis, 0, len(suite.VersionAxes))
		for _, axis := range suite.VersionAxes {
			axes = append(axes, axis.versionFilterAxis())
		}
		paths, err = versionfilter.SelectScenarios(
			versionfilter.FeatureSource{FS: suite.Source.FS, Paths: suite.Source.Paths},
			axes...,
		)
		if err != nil {
			return nil, err
		}
	}
	log.Printf("acceptance: target Soperator version=%s", info.TargetSoperatorVersion)
	log.Printf("acceptance: suite %s running scenarios: %s", suite.Name, strings.Join(paths, ", "))
	return paths, nil
}

func (r *Runner) runSuite(ctx context.Context, info *framework.ClusterInfo, runtime framework.Runtime, suite SuiteConfig, features []string) error {
	format, err := reports.Format(r.reportDir, suite.Name)
	if err != nil {
		return fmt.Errorf("suite %q report format: %w", suite.Name, err)
	}

	tags := r.suiteTagFilter(suite)
	godogSuite := godog.TestSuite{
		Name: suite.Name,
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			r.initializeSuiteScenario(sc, info, runtime, suite)
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

func (r *Runner) initializeSuiteScenario(sc *godog.ScenarioContext, info *framework.ClusterInfo, runtime framework.Runtime, suite SuiteConfig) {
	registerTimingHooks(sc)
	registerSkipHook(sc)
	for _, register := range suite.StepRegistrars {
		register(sc, info, runtime)
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

func newWorld(kubectlContext, slurmClusterName, soperatorVersion string) *world {
	return &world{
		logPrefix:        "acceptance",
		kubectlContext:   kubectlContext,
		slurmClusterName: slurmClusterName,
		soperatorVersion: soperatorVersion,
	}
}

func (w *world) logf(format string, args ...any) {
	log.Printf("%s: %s", w.logPrefix, fmt.Sprintf(format, args...))
}
