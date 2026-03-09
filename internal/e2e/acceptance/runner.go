package acceptance

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

const (
	phasePreDestroy  = "pre-destroy"
	phasePostDestroy = "post-destroy"
)

type Runner struct {
	cfg   Config
	phase string
}

type Config struct {
	NebiusProjectID string
	ClusterName     string
}

func NewRunner(cfg Config, phase string) (*Runner, error) {
	if phase == "" {
		phase = phasePreDestroy
	}

	switch phase {
	case phasePreDestroy, phasePostDestroy:
		return &Runner{cfg: cfg, phase: phase}, nil
	default:
		return nil, fmt.Errorf("unknown acceptance phase %q", phase)
	}
}

func (r *Runner) Run(ctx context.Context) error {
	features, err := featurePaths(r.phase)
	if err != nil {
		return err
	}

	suite := godog.TestSuite{
		Name:                "soperator-acceptance-" + r.phase,
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

func featurePaths(phase string) ([]string, error) {
	baseDir := filepath.Join("internal", "e2e", "acceptance", "features")
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("read features directory: %w", err)
	}

	suffix := "." + phase + ".feature"
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		paths = append(paths, filepath.Join(baseDir, entry.Name()))
	}

	if len(paths) == 0 {
		return nil, errors.New("no feature files found for phase " + phase)
	}

	return paths, nil
}

func (r *Runner) initializeScenario(sc *godog.ScenarioContext) {
	world := newWorld(r.cfg)

	sc.Step(`^the provisioned Slurm cluster is reachable$`, world.theProvisionedSlurmClusterIsReachable)
	sc.Step(`^a regular user can SSH from the login node to a worker without extra SSH options$`, world.aRegularUserCanSSHFromTheLoginNodeToAWorkerWithoutExtraSSHOptions)
	sc.Step(`^packages can be installed on the worker without breaking the NVIDIA driver$`, world.packagesCanBeInstalledOnTheWorkerWithoutBreakingTheNVIDIADriver)
	sc.Step(`^a maintenance event replaces the worker node and returns it to service$`, world.aMaintenanceEventReplacesTheWorkerNodeAndReturnsItToService)
	sc.Step(`^the workflow destroy step removes the e2e cluster$`, world.theWorkflowDestroyStepRemovesTheE2ECluster)
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
