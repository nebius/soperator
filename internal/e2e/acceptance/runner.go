package acceptance

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
	"nebius.ai/slurm-operator/internal/e2e/acceptance/steps"
)

//go:embed features/*.feature
var acceptanceFeatures embed.FS

type timingCtxKey string

const (
	scenarioStartTimeKey timingCtxKey = "acceptance_scenario_start_time"
	stepStartTimeKey     timingCtxKey = "acceptance_step_start_time"
)

type Runner struct {
	state            *framework.ClusterState
	runUnstableTests bool
}

func NewRunner(state *framework.ClusterState, runUnstableTests bool) *Runner {
	if state == nil {
		state = &framework.ClusterState{
			WorkersByNodeSet: make(map[string][]framework.WorkerRef),
		}
	}
	if state.WorkersByNodeSet == nil {
		state.WorkersByNodeSet = make(map[string][]framework.WorkerRef)
	}
	return &Runner{
		state:            state,
		runUnstableTests: runUnstableTests,
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

	tags := r.tagFilter()

	format := "pretty"
	if dir := os.Getenv("E2E_REPORT_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create report dir %q: %w", dir, err)
		}
		format = fmt.Sprintf("pretty,cucumber:%s,junit:%s",
			filepath.Join(dir, "acceptance.cucumber.json"),
			filepath.Join(dir, "acceptance.junit.xml"),
		)
	}

	suite := godog.TestSuite{
		Name:                "soperator-acceptance",
		ScenarioInitializer: r.initializeScenario,
		Options: &godog.Options{
			Format:         format,
			FS:             acceptanceFeatures,
			Paths:          features,
			TestingT:       nil,
			Strict:         true,
			DefaultContext: ctx,
			Tags:           tags,
		},
	}

	suiteStart := time.Now()
	status := suite.Run()
	log.Printf("acceptance: suite finished duration=%s", time.Since(suiteStart).Round(time.Millisecond))
	if status != 0 {
		return fmt.Errorf("godog suite exited with status %d", status)
	}

	return nil
}

func (r *Runner) tagFilter() string {
	var filters []string

	if !r.runUnstableTests {
		log.Printf("acceptance: RUN_UNSTABLE_TESTS=false, excluding @unstable scenarios")
		filters = append(filters, "~@unstable")
	}
	if !r.state.HasGPUWorkers() {
		log.Printf("acceptance: no GPU workers found, excluding @gpu scenarios")
		filters = append(filters, "~@gpu")
	}

	return strings.Join(filters, " && ")
}

func discoverCluster(ctx context.Context, w *world, state *framework.ClusterState) error {
	if _, err := framework.RunWithDefaultRetry(ctx, w, "kubectl", "get", "pods", "-n", soperatorNamespace); err != nil {
		return err
	}
	if err := verifyPodReady(ctx, w, soperatorNamespace, "login-0"); err != nil {
		return fmt.Errorf("verify login pod: %w", err)
	}
	if err := verifyPodReady(ctx, w, soperatorNamespace, "controller-0"); err != nil {
		return fmt.Errorf("verify controller pod: %w", err)
	}
	if _, err := framework.ExecControllerWithDefaultRetry(ctx, w, "true"); err != nil {
		return fmt.Errorf("exec controller sanity check: %w", err)
	}
	if _, err := framework.ExecJailWithDefaultRetry(ctx, w, "true"); err != nil {
		return fmt.Errorf("exec login jail sanity check: %w", err)
	}

	workerOutput, err := framework.ExecControllerWithDefaultRetry(ctx, w, `sinfo -hN -p main -o '%N'`)
	if err != nil {
		return fmt.Errorf("discover worker nodes: %w", err)
	}

	seen := make(map[string]struct{})
	var workers []framework.WorkerRef
	for _, line := range strings.Split(workerOutput, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		workers = append(workers, framework.WorkerRef{Name: name})
	}
	if len(workers) == 0 {
		return fmt.Errorf("no worker nodes discovered")
	}
	state.Workers = workers
	classifyWorkers(state)

	log.Printf("acceptance: discovered workers: %s", workerNames(state.Workers))
	log.Printf("acceptance: discovered GPU workers: %s", workerNames(state.GPUWorkers))
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
	registerTimingHooks(sc)

	w := newWorld(r.state)

	steps.NewClusterCreation(r.state, w).Register(sc)
	steps.NewInternalSSH(w).Register(sc)
	steps.NewPackageInstallation(w).Register(sc)
	steps.NewNodeReplacement(w).Register(sc)
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
	if len(workers) == 0 {
		return "<none>"
	}
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		names = append(names, worker.Name)
	}
	return strings.Join(names, ", ")
}

func verifyPodReady(ctx context.Context, w *world, namespace, name string) error {
	output, err := framework.RunWithDefaultRetry(ctx, w, "kubectl", "get", "pod", "-n", namespace, name, "-o", "json")
	if err != nil {
		return err
	}

	var pod corev1.Pod
	if err := json.Unmarshal([]byte(output), &pod); err != nil {
		return fmt.Errorf("decode pod %s/%s: %w", namespace, name, err)
	}
	if pod.Status.Phase != corev1.PodRunning {
		return fmt.Errorf("pod %s/%s phase=%s, want %s", namespace, name, pod.Status.Phase, corev1.PodRunning)
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return nil
		}
	}
	return fmt.Errorf("pod %s/%s is not Ready", namespace, name)
}

func classifyWorkers(state *framework.ClusterState) {
	state.WorkersByNodeSet = make(map[string][]framework.WorkerRef, len(state.ExpectedNodeSets))
	state.GPUWorkers = nil

	if len(state.ExpectedNodeSets) == 0 {
		return
	}

	expected := slices.Clone(state.ExpectedNodeSets)
	sort.Slice(expected, func(i, j int) bool {
		return len(expected[i].Name) > len(expected[j].Name)
	})

	gpuByName := make(map[string]bool, len(expected))
	for _, nodeSet := range expected {
		gpuByName[nodeSet.Name] = nodeSet.HasGPU
	}

	for _, worker := range state.Workers {
		for _, nodeSet := range expected {
			prefix := nodeSet.Name + "-"
			if !strings.HasPrefix(worker.Name, prefix) {
				continue
			}
			state.WorkersByNodeSet[nodeSet.Name] = append(state.WorkersByNodeSet[nodeSet.Name], worker)
			if gpuByName[nodeSet.Name] {
				state.GPUWorkers = append(state.GPUWorkers, worker)
			}
			break
		}
	}
}
