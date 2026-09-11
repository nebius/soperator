package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance"
	"github.com/nebius/soperator/e2e/acceptance/artifacts"
)

const defaultSlurmClusterName = "soperator"

type runOptions struct {
	KubectlContext    string
	SlurmClusterName  string
	SoperatorVersion  string
	RunUnstableTests  bool
	RunEssentialTests bool
	ScenarioPaths     []string
	OutputDir         string
}

type collectOptions struct {
	KubectlContext   string
	SlurmClusterName string
	SoperatorVersion string
	NebiusProjectID  string
	OutputDir        string
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

// Run parses an acceptance subcommand and executes it.
func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("subcommand is required: use run or collect-artifacts")
	}

	switch args[0] {
	case "run":
		return runAcceptance(ctx, args[1:])
	case "collect-artifacts":
		return collectArtifacts(ctx, args[1:])
	case "help", "-h", "--help":
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q: use run or collect-artifacts", args[0])
	}
}

func runAcceptance(ctx context.Context, args []string) error {
	opts, err := parseRunOptions(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("parse run args: %w", err)
	}

	targetVersion, err := resolveTargetSoperatorVersion(ctx, opts.KubectlContext, opts.SoperatorVersion)
	if err != nil {
		return err
	}
	runner, err := acceptance.NewRunner(acceptance.RunnerConfig{
		KubectlContext:         opts.KubectlContext,
		SlurmClusterName:       opts.SlurmClusterName,
		TargetSoperatorVersion: targetVersion,
		OutputDir:              opts.OutputDir,
		Suites:                 []acceptance.SuiteConfig{suiteFromOptions(opts, targetVersion)},
	})
	if err != nil {
		return err
	}
	return runner.Run(ctx)
}

func collectArtifacts(ctx context.Context, args []string) error {
	opts, err := parseCollectOptions(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("parse collect-artifacts args: %w", err)
	}
	if opts.KubectlContext == "" {
		log.Printf("artifacts: skip Kubernetes-bound collectors: kubectl context is not provided")
		return artifacts.CollectAll(ctx, opts.OutputDir,
			artifacts.NewMK8sCollector(acceptance.NewLocalArgsScope(), opts.NebiusProjectID),
		)
	}
	targetVersion, err := resolveTargetSoperatorVersion(ctx, opts.KubectlContext, opts.SoperatorVersion)
	if err != nil {
		return err
	}
	runtime, err := acceptance.NewRuntime(opts.KubectlContext, opts.SlurmClusterName, targetVersion)
	if err != nil {
		return err
	}
	collectors := artifacts.CommonCollectors(runtime, opts.NebiusProjectID)
	return artifacts.CollectAll(ctx, opts.OutputDir, collectors...)
}

func suiteFromOptions(opts runOptions, targetSoperatorVersion string) acceptance.SuiteConfig {
	suite := acceptance.SoperatorSuite(targetSoperatorVersion)
	if len(opts.ScenarioPaths) > 0 {
		suite.Source.Paths = opts.ScenarioPaths
	}
	if opts.RunEssentialTests {
		suite.Tags = "@essential"
	}
	if opts.RunUnstableTests {
		suite.ExcludeUnstable = false
	}
	return suite
}

func parseRunOptions(args []string) (runOptions, error) {
	opts := runOptions{SlurmClusterName: defaultSlurmClusterName}
	fs := flag.NewFlagSet("acceptance run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&opts.KubectlContext, "kubectl-context", "", "kubectl context to use for acceptance tests")
	fs.StringVar(&opts.SlurmClusterName, "slurm-cluster-name", opts.SlurmClusterName, "SlurmCluster resource name")
	fs.StringVar(&opts.SoperatorVersion, "soperator-version", "", "target Soperator version; when omitted, Flux HelmRelease discovery is used")
	fs.BoolVar(&opts.RunUnstableTests, "run-unstable", false, "run scenarios tagged @unstable")
	fs.BoolVar(&opts.RunEssentialTests, "run-essential", false, "run only scenarios tagged @essential")
	fs.Var((*scenarioPathFlag)(&opts.ScenarioPaths), "scenario", "feature file or exact Scenario line to run; may be repeated")
	fs.StringVar(&opts.OutputDir, "output-dir", "", "directory for reports and per-scenario artifacts")
	if err := fs.Parse(args); err != nil {
		return runOptions{}, err
	}
	if fs.NArg() > 0 {
		return runOptions{}, fmt.Errorf("unexpected run arguments: %s", strings.Join(fs.Args(), " "))
	}
	trimRunOptions(&opts)
	if err := validateCommonOptions(opts.KubectlContext, opts.OutputDir); err != nil {
		return runOptions{}, err
	}
	return opts, nil
}

func parseCollectOptions(args []string) (collectOptions, error) {
	opts := collectOptions{SlurmClusterName: defaultSlurmClusterName}
	fs := flag.NewFlagSet("acceptance collect-artifacts", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&opts.KubectlContext, "kubectl-context", "", "optional kubectl context; when omitted, only MK8s artifacts are collected")
	fs.StringVar(&opts.SlurmClusterName, "slurm-cluster-name", opts.SlurmClusterName, "SlurmCluster resource name")
	fs.StringVar(&opts.SoperatorVersion, "soperator-version", "", "target Soperator version; when omitted, Flux HelmRelease discovery is used")
	fs.StringVar(&opts.NebiusProjectID, "nebius-project-id", "", "optional Nebius project ID for Managed Kubernetes artifacts")
	fs.StringVar(&opts.OutputDir, "output-dir", "", "directory for collected artifacts")
	if err := fs.Parse(args); err != nil {
		return collectOptions{}, err
	}
	if fs.NArg() > 0 {
		return collectOptions{}, fmt.Errorf("unexpected collect-artifacts arguments: %s", strings.Join(fs.Args(), " "))
	}
	opts.KubectlContext = strings.TrimSpace(opts.KubectlContext)
	opts.SlurmClusterName = strings.TrimSpace(opts.SlurmClusterName)
	opts.SoperatorVersion = strings.TrimSpace(opts.SoperatorVersion)
	opts.NebiusProjectID = strings.TrimSpace(opts.NebiusProjectID)
	opts.OutputDir = strings.TrimSpace(opts.OutputDir)
	if opts.OutputDir == "" {
		return collectOptions{}, fmt.Errorf("--output-dir is required")
	}
	if opts.KubectlContext == "" && opts.NebiusProjectID == "" {
		return collectOptions{}, fmt.Errorf("at least one of --kubectl-context or --nebius-project-id is required")
	}
	return opts, nil
}

func trimRunOptions(opts *runOptions) {
	opts.KubectlContext = strings.TrimSpace(opts.KubectlContext)
	opts.SlurmClusterName = strings.TrimSpace(opts.SlurmClusterName)
	opts.SoperatorVersion = strings.TrimSpace(opts.SoperatorVersion)
	opts.OutputDir = strings.TrimSpace(opts.OutputDir)
}

func validateCommonOptions(kubectlContext, outputDir string) error {
	if kubectlContext == "" {
		return fmt.Errorf("--kubectl-context is required")
	}
	if outputDir == "" {
		return fmt.Errorf("--output-dir is required")
	}
	return nil
}

func resolveTargetSoperatorVersion(ctx context.Context, kubectlContext, configuredVersion string) (string, error) {
	if configuredVersion != "" {
		return configuredVersion, nil
	}
	version, err := discoverFluxSoperatorVersion(ctx, kubectlContext)
	if err != nil {
		return "", fmt.Errorf("discover target Soperator version from Flux HelmRelease: %w; pass --soperator-version to override", err)
	}
	return version, nil
}
