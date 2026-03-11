package acceptance

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
	"nebius.ai/slurm-operator/internal/e2e/acceptance/steps"
)

type Runner struct {
	cfg   Config
	state *framework.ClusterState
}

type Config struct {
	NebiusProjectID string
	ClusterName     string
}

func NewRunner(cfg Config) *Runner {
	return &Runner{
		cfg:   cfg,
		state: &framework.ClusterState{},
	}
}

func (r *Runner) Run(ctx context.Context) error {
	features := featurePaths()
	if len(features) == 0 {
		return fmt.Errorf("no acceptance feature files configured")
	}

	suite := godog.TestSuite{
		Name:                 "soperator-acceptance",
		TestSuiteInitializer: r.initializeSuite(ctx),
		ScenarioInitializer:  r.initializeScenario,
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

func (r *Runner) initializeSuite(ctx context.Context) func(*godog.TestSuiteContext) {
	return func(sc *godog.TestSuiteContext) {
		sc.BeforeSuite(func() {
			w := newWorld(r.cfg, r.state)
			if err := discoverCluster(ctx, w, r.state); err != nil {
				log.Fatalf("BeforeSuite cluster discovery: %v", err)
			}
		})
	}
}

func discoverCluster(ctx context.Context, w *world, state *framework.ClusterState) error {
	if _, err := w.Run(ctx, "kubectl", "get", "pods", "-n", soperatorNamespace); err != nil {
		return err
	}
	if _, err := w.Run(ctx, "kubectl", "get", "pod", "-n", soperatorNamespace, "login-0"); err != nil {
		return err
	}
	if _, err := w.Run(ctx, "kubectl", "get", "pod", "-n", soperatorNamespace, "controller-0"); err != nil {
		return err
	}

	workerOutput, err := w.ExecController(ctx, `sinfo -hN -p main -o '%N'`)
	if err != nil {
		return fmt.Errorf("discover worker nodes: %w", err)
	}

	var workers []framework.WorkerRef
	for _, line := range strings.Split(workerOutput, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		workers = append(workers, framework.WorkerRef{Name: name})
	}
	if len(workers) == 0 {
		return fmt.Errorf("no worker nodes discovered")
	}
	state.Workers = workers

	log.Printf("acceptance: discovered workers: %s", workerNames(workers))
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
	w := newWorld(r.cfg, r.state)

	steps.NewClusterCreation(r.state, w).Register(sc)
	steps.NewInternalSSH(w).Register(sc)
	steps.NewPackageInstallation(w).Register(sc)
	steps.NewNodeReplacement(w, sc).Register(sc)
}

func newWorld(cfg Config, state *framework.ClusterState) *world {
	return &world{
		cfg:            cfg,
		commandTimeout: 10 * time.Minute,
		logPrefix:      "acceptance",
		state:          state,
	}
}

func (w *world) logf(format string, args ...any) {
	log.Printf("%s: %s", w.logPrefix, fmt.Sprintf(format, args...))
}

func workerNames(workers []framework.WorkerRef) string {
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		names = append(names, worker.Name)
	}
	return strings.Join(names, ", ")
}
