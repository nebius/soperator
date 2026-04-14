package acceptance

import (
	"context"
	"embed"
	"fmt"
	"log"
	"strings"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
	"nebius.ai/slurm-operator/internal/e2e/acceptance/steps"
)

//go:embed features/*.feature
var acceptanceFeatures embed.FS

type Runner struct {
	state *framework.ClusterState
}

func NewRunner() *Runner {
	return &Runner{
		state: &framework.ClusterState{},
	}
}

func (r *Runner) Run(ctx context.Context) error {
	w := newWorld(r.state)
	if err := discoverCluster(ctx, w, r.state); err != nil {
		return fmt.Errorf("discover cluster before suite: %w", err)
	}

	features := featurePaths()
	if len(features) == 0 {
		return fmt.Errorf("no acceptance feature files configured")
	}

	tags := ""
	if !r.state.HasGPUWorkers() {
		log.Printf("acceptance: no GPU workers found, excluding @gpu scenarios")
		tags = "~@gpu"
	}

	suite := godog.TestSuite{
		Name:                "soperator-acceptance",
		ScenarioInitializer: r.initializeScenario,
		Options: &godog.Options{
			Format:         "pretty",
			FS:             acceptanceFeatures,
			Paths:          features,
			TestingT:       nil,
			Strict:         true,
			DefaultContext: ctx,
			Tags:           tags,
		},
	}

	if status := suite.Run(); status != 0 {
		return fmt.Errorf("godog suite exited with status %d", status)
	}

	return nil
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

	workerOutput, err := w.ExecController(ctx, `sinfo -hN -p main -o '%N|%G'`)
	if err != nil {
		return fmt.Errorf("discover worker nodes: %w", err)
	}

	var workers []framework.WorkerRef
	for _, line := range strings.Split(workerOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		ref := framework.WorkerRef{Name: name}
		if len(parts) > 1 && strings.Contains(parts[1], "gpu") {
			ref.HasGPU = true
		}
		workers = append(workers, ref)
	}
	if len(workers) == 0 {
		return fmt.Errorf("no worker nodes discovered")
	}
	state.Workers = workers

	log.Printf("acceptance: discovered workers: %s", workerNames(workers))
	return nil
}

func featurePaths() []string {
	return []string{
		"features/cluster_creation.feature",
		"features/internal_ssh.feature",
		"features/package_installation.feature",
		"features/node_replacement.feature",
	}
}

func (r *Runner) initializeScenario(sc *godog.ScenarioContext) {
	w := newWorld(r.state)

	steps.NewClusterCreation(r.state, w).Register(sc)
	steps.NewInternalSSH(w).Register(sc)
	steps.NewPackageInstallation(w).Register(sc)
	steps.NewNodeReplacement(w).Register(sc)
}

func newWorld(state *framework.ClusterState) *world {
	return &world{
		logPrefix: "acceptance",
		state:     state,
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
