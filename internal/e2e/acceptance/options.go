package acceptance

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const defaultSlurmClusterName = "soperator"

type options struct {
	KubectlContext   string
	SlurmClusterName string
	RunUnstableTests bool
	ReportDir        string
}

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
		WorkersByNodeSet: make(map[string][]framework.WorkerPodRef),
	}

	runner := NewRunner(state, opts.RunUnstableTests, opts.KubectlContext, opts.ReportDir)
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
	fs.BoolVar(&opts.RunUnstableTests, "run-unstable", false, "run scenarios tagged @unstable")
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
	opts.ReportDir = strings.TrimSpace(opts.ReportDir)

	return opts, nil
}
