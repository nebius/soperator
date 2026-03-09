package acceptance

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/cucumber/godog"
)

type Runner struct {
	cfg Config
}

type Config struct {
	NebiusProjectID string
	ClusterName     string
}

func NewRunner(cfg Config) (*Runner, error) {
	return &Runner{cfg: cfg}, nil
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
			Format:         "pretty",
			Paths:          features,
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
	world := newWorld(r.cfg)

	sc.Step(`^the provisioned Slurm cluster is reachable$`, world.theProvisionedSlurmClusterIsReachable)
	sc.Step(`^a regular user can SSH from the login node to a worker without extra SSH options$`, world.aRegularUserCanSSHFromTheLoginNodeToAWorkerWithoutExtraSSHOptions)
	sc.Step(`^packages can be installed on the worker without breaking the NVIDIA driver$`, world.packagesCanBeInstalledOnTheWorkerWithoutBreakingTheNVIDIADriver)
	sc.Step(`^a maintenance event replaces the worker node and returns it to service$`, world.aMaintenanceEventReplacesTheWorkerNodeAndReturnsItToService)
}

func newWorld(cfg Config) *world {
	return &world{
		cfg:              cfg,
		commandTimeout:   10 * time.Minute,
		pollInterval:     10 * time.Second,
		replacementDelay: 25 * time.Minute,
		logPrefix:        "acceptance",
	}
}

func (w *world) logf(format string, args ...any) {
	log.Printf("%s: %s", w.logPrefix, fmt.Sprintf(format, args...))
}
