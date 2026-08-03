package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"nebius.ai/soperator-e2e/acceptance"
	"nebius.ai/soperator-e2e/acceptance/framework"
)

const defaultSlurmClusterName = "soperator"

type options struct {
	KubectlContext   string
	SlurmClusterName string
	SoperatorVersion string
	RunUnstableTests bool
	ScenarioPaths    []string
	ReportDir        string
}

type scenarioPathFlag []string

func (f *scenarioPathFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *scenarioPathFlag) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("--scenario value cannot be empty")
	}
	*f = append(*f, trimmed)
	return nil
}

// Run parses CLI-style acceptance arguments and runs the shared acceptance suite.
func Run(ctx context.Context, args []string) error {
	opts, err := parseOptions(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("parse acceptance args: %w", err)
	}

	state := &framework.ClusterState{
		SlurmClusterName: opts.SlurmClusterName,
		WorkersByNodeSet: make(map[string][]framework.WorkerRef),
	}
	targetSoperatorVersion, err := resolveTargetSoperatorVersion(ctx, opts)
	if err != nil {
		return err
	}

	features := acceptance.SharedFeatureSource()
	if len(opts.ScenarioPaths) > 0 {
		features.Paths = opts.ScenarioPaths
	}

	runner, err := acceptance.NewRunner(acceptance.Options{
		KubectlContext:            opts.KubectlContext,
		SlurmClusterName:          opts.SlurmClusterName,
		TargetSoperatorVersion:    targetSoperatorVersion,
		ReportDir:                 opts.ReportDir,
		Features:                  features,
		ExcludeUnstable:           !opts.RunUnstableTests,
		ExcludeMissingWorkerKinds: true,
		State:                     state,
		StepRegistrars:            []acceptance.StepRegistrar{acceptance.SharedStepRegistrar()},
	})
	if err != nil {
		return err
	}
	return runner.Run(ctx)
}

func parseOptions(args []string) (options, error) {
	opts := options{
		SlurmClusterName: defaultSlurmClusterName,
	}

	fs := flag.NewFlagSet("acceptance", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&opts.KubectlContext, "kubectl-context", "", "kubectl context to use for acceptance tests")
	fs.StringVar(&opts.SlurmClusterName, "slurm-cluster-name", opts.SlurmClusterName, "SlurmCluster resource name")
	fs.StringVar(&opts.SoperatorVersion, "soperator-version", "", "target Soperator version; when omitted, Flux HelmRelease discovery is used")
	fs.BoolVar(&opts.RunUnstableTests, "run-unstable", false, "run scenarios tagged @unstable")
	fs.Var((*scenarioPathFlag)(&opts.ScenarioPaths), "scenario", "feature file or exact Scenario line to run, e.g. features/internal_ssh.feature:3; may be repeated")
	fs.StringVar(&opts.ReportDir, "report-dir", "", "optional directory for Cucumber and JUnit reports")

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected acceptance arguments: %s", strings.Join(fs.Args(), " "))
	}

	opts.KubectlContext = strings.TrimSpace(opts.KubectlContext)
	if opts.KubectlContext == "" {
		return options{}, fmt.Errorf("--kubectl-context is required")
	}
	opts.SlurmClusterName = strings.TrimSpace(opts.SlurmClusterName)
	opts.SoperatorVersion = strings.TrimSpace(opts.SoperatorVersion)
	opts.ReportDir = strings.TrimSpace(opts.ReportDir)

	return opts, nil
}

func resolveTargetSoperatorVersion(ctx context.Context, opts options) (string, error) {
	if opts.SoperatorVersion != "" {
		return opts.SoperatorVersion, nil
	}

	version, err := discoverFluxSoperatorVersion(ctx, opts.KubectlContext)
	if err != nil {
		return "", fmt.Errorf("discover target Soperator version from Flux HelmRelease: %w; pass --soperator-version to override", err)
	}
	return version, nil
}
