package acceptance

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
	"nebius.ai/slurm-operator/internal/e2e/acceptance/steps"
)

type Runner struct {
	cfg Config
}

type Config struct {
	NebiusProjectID string
	ClusterName     string
}

func NewRunner(cfg Config) *Runner {
	return &Runner{cfg: cfg}
}

func (r *Runner) Run(ctx context.Context) error {
	features := featurePaths()
	if len(features) == 0 {
		return fmt.Errorf("no acceptance feature files configured")
	}

	suite := godog.TestSuite{
		Name:                "soperator-acceptance",
		ScenarioInitializer: r.initializeScenario,
		Options: &godog.Options{
			Format: "pretty",
			Paths:  features,
			// Future option: enable StopOnFailure here if we want fast-fail acceptance runs.
			// Future option: write per-scenario logs to files and upload them as CI artifacts.
			TestingT:       nil,
			Strict:         true,
			DefaultContext: ctx,
		},
	}

	if status := suite.Run(); status != 0 {
		return fmt.Errorf("godog suite exited with status %d", status)
	}

	return nil
}

func featurePaths() []string {
	baseDir := filepath.Join("internal", "e2e", "acceptance", "features")
	return []string{
		filepath.Join(baseDir, "cluster_creation.feature"),
		filepath.Join(baseDir, "internal_ssh.feature"),
		filepath.Join(baseDir, "package_installation.feature"),
		filepath.Join(baseDir, "node_replacement.feature"),
	}
}

func (r *Runner) initializeScenario(sc *godog.ScenarioContext) {
	state := &framework.SharedState{
		InternalSSH: framework.InternalSSHConfig{
			UserName: "bob",
		},
	}
	world := newWorld(r.cfg, state)

	steps.NewClusterCreation(state, world).Register(sc)
	steps.NewInternalSSH(state, world).Register(sc)
	steps.NewPackageInstallation(state, world).Register(sc)
	steps.NewNodeReplacement(state, world).Register(sc)
}

func newWorld(cfg Config, state *framework.SharedState) *world {
	return &world{
		cfg:              cfg,
		commandTimeout:   10 * time.Minute,
		replacementDelay: 25 * time.Minute,
		logPrefix:        "acceptance",
		state:            state,
	}
}

func (w *world) logf(format string, args ...any) {
	log.Printf("%s: %s", w.logPrefix, fmt.Sprintf(format, args...))
}
