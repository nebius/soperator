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

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
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
	kubectlContext   string
	reportDir        string
}

func NewRunner(state *framework.ClusterState, runUnstableTests bool, kubectlContext, reportDir string) *Runner {
	if state == nil {
		state = &framework.ClusterState{
			WorkersByNodeSet: make(map[string][]framework.WorkerPodRef),
		}
	}
	if state.WorkersByNodeSet == nil {
		state.WorkersByNodeSet = make(map[string][]framework.WorkerPodRef)
	}
	return &Runner{
		state:            state,
		runUnstableTests: runUnstableTests,
		kubectlContext:   kubectlContext,
		reportDir:        reportDir,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	w := newWorld(r.state, r.kubectlContext)
	if err := discoverCluster(ctx, w, r.state); err != nil {
		return fmt.Errorf("discover cluster before suite: %w", err)
	}

	features := featurePaths()
	if len(features) == 0 {
		return fmt.Errorf("no acceptance feature files configured")
	}

	tags := r.tagFilter()

	format, err := reportFormat(r.reportDir)
	if err != nil {
		return err
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
		log.Printf("acceptance: run-unstable=false, excluding @unstable scenarios")
		filters = append(filters, "~@unstable")
	}
	if !r.state.HasGPUWorkers() {
		log.Printf("acceptance: no GPU workers found, excluding @gpu scenarios")
		filters = append(filters, "~@gpu")
	}
	if !r.state.HasHeterogeneousWorkers() {
		log.Printf("acceptance: no heterogeneous CPU+GPU worker set found, excluding @heterogeneous scenarios")
		filters = append(filters, "~@heterogeneous")
	}

	return strings.Join(filters, " && ")
}

func discoverCluster(ctx context.Context, w *world, state *framework.ClusterState) error {
	if _, err := w.Kubectl().RunWithDefaultRetry(ctx, "get", "pods", "-n", soperatorNamespace); err != nil {
		return err
	}
	if err := verifyPodReady(ctx, w, soperatorNamespace, state.PodName("login-0")); err != nil {
		return fmt.Errorf("verify login pod: %w", err)
	}
	if err := verifyPodReady(ctx, w, soperatorNamespace, state.PodName("controller-0")); err != nil {
		return fmt.Errorf("verify controller pod: %w", err)
	}
	if _, err := w.Controller().RunWithDefaultRetry(ctx, "true"); err != nil {
		return fmt.Errorf("exec controller sanity check: %w", err)
	}
	if _, err := w.Jail().RunWithDefaultRetry(ctx, "true"); err != nil {
		return fmt.Errorf("exec login jail sanity check: %w", err)
	}

	discoveredNodeSets, err := discoverNodeSets(ctx, w, state.SlurmClusterName)
	if err != nil {
		return fmt.Errorf("discover NodeSets: %w", err)
	}
	state.DiscoveredNodeSets = discoveredNodeSets
	log.Printf("acceptance: discovered nodesets: %s", discoveredNodeSetSummary(state.DiscoveredNodeSets))

	workerOutput, err := w.Controller().RunWithDefaultRetry(ctx, `sinfo -hN -p main -o '%N'`)
	if err != nil {
		return fmt.Errorf("discover worker nodes: %w", err)
	}

	seen := make(map[string]struct{})
	var workers []framework.WorkerPodRef
	for _, line := range strings.Split(workerOutput, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		workers = append(workers, framework.WorkerPodRef{Name: name})
	}
	if len(workers) == 0 {
		return fmt.Errorf("no worker nodes discovered")
	}
	state.Workers = workers
	classifyWorkers(state)
	if err := verifyDiscoveredWorkers(state); err != nil {
		return err
	}

	log.Printf("acceptance: discovered workers: %s", workerNames(state.Workers))
	log.Printf("acceptance: discovered CPU workers: %s", workerNames(state.CPUWorkers))
	log.Printf("acceptance: discovered GPU workers: %s", workerNames(state.GPUWorkers))
	log.Printf("acceptance: discovered workers by nodeset: %s", workersByNodeSetSummary(state.WorkersByNodeSet))
	return nil
}

func reportFormat(reportDir string) (string, error) {
	if reportDir == "" {
		return "pretty", nil
	}
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir %q: %w", reportDir, err)
	}
	return fmt.Sprintf("pretty,cucumber:%s,junit:%s",
		filepath.Join(reportDir, "acceptance.cucumber.json"),
		filepath.Join(reportDir, "acceptance.junit.xml"),
	), nil
}

func discoverNodeSets(ctx context.Context, w *world, clusterName string) ([]framework.DiscoveredNodeSet, error) {
	output, err := w.Kubectl().RunWithDefaultRetry(ctx, "get", "nodesets", "-n", soperatorNamespace, "-o", "json")
	if err != nil {
		return nil, err
	}

	var nodeSets slurmv1alpha1.NodeSetList
	if err := json.Unmarshal([]byte(output), &nodeSets); err != nil {
		return nil, fmt.Errorf("decode NodeSet list: %w", err)
	}

	discovered := discoveredNodeSetsFromLiveList(nodeSets, clusterName)
	if len(discovered) == 0 {
		return nil, fmt.Errorf("no NodeSets found in namespace %s for Slurm cluster %q", soperatorNamespace, clusterName)
	}
	return discovered, nil
}

func discoveredNodeSetsFromLiveList(nodeSets slurmv1alpha1.NodeSetList, clusterName string) []framework.DiscoveredNodeSet {
	discovered := make([]framework.DiscoveredNodeSet, 0, len(nodeSets.Items))
	for _, nodeSet := range nodeSets.Items {
		if clusterName != "" && nodeSet.Spec.ClusterName != "" && nodeSet.Spec.ClusterName != clusterName {
			continue
		}
		discovered = append(discovered, framework.DiscoveredNodeSet{
			Name:   nodeSet.Name,
			Size:   int(nodeSet.Spec.Replicas),
			HasGPU: nodeSet.Spec.GPU.Enabled,
		})
	}

	sort.Slice(discovered, func(i, j int) bool {
		return discovered[i].Name < discovered[j].Name
	})
	return discovered
}

func featurePaths() []string {
	return []string{
		// "features/cluster_creation.feature",
		// "features/internal_ssh.feature",
		// "features/package_installation.feature",
		"features/node_replacement.feature",
		// "features/docker_containers.feature",
		// "features/enroot_containers.feature",
		// "features/topology.feature",
	}
}

func (r *Runner) initializeScenario(sc *godog.ScenarioContext) {
	registerTimingHooks(sc)

	w := newWorld(r.state, r.kubectlContext)
	slurm := framework.NewSlurmClient(w)

	steps.NewClusterCreation(r.state, w).Register(sc)
	steps.NewInternalSSH(w, slurm).Register(sc)
	steps.NewPackageInstallation(w, slurm).Register(sc)
	steps.NewNodeReplacement(w, slurm).Register(sc)
	steps.NewDockerContainers(w, slurm, r.state.SlurmClusterName).Register(sc)
	steps.NewEnrootContainers(w, slurm, r.state.SlurmClusterName).Register(sc)
	steps.NewTopology(r.state, w).Register(sc)

	registerSkipHook(sc)
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

func newWorld(state *framework.ClusterState, kubectlContext string) *world {
	return &world{
		logPrefix:      "acceptance",
		state:          state,
		kubectlContext: kubectlContext,
	}
}

func (w *world) logf(format string, args ...any) {
	log.Printf("%s: %s", w.logPrefix, fmt.Sprintf(format, args...))
}

func workerNames(workers []framework.WorkerPodRef) string {
	if len(workers) == 0 {
		return "<none>"
	}
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		names = append(names, worker.Name)
	}
	return strings.Join(names, ", ")
}

func workersByNodeSetSummary(workersByNodeSet map[string][]framework.WorkerPodRef) string {
	if len(workersByNodeSet) == 0 {
		return "<none>"
	}

	names := make([]string, 0, len(workersByNodeSet))
	for nodeSet := range workersByNodeSet {
		names = append(names, nodeSet)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, nodeSet := range names {
		parts = append(parts, fmt.Sprintf("%s=[%s]", nodeSet, workerNames(workersByNodeSet[nodeSet])))
	}

	return strings.Join(parts, "; ")
}

func discoveredNodeSetSummary(nodeSets []framework.DiscoveredNodeSet) string {
	if len(nodeSets) == 0 {
		return "<none>"
	}
	parts := make([]string, 0, len(nodeSets))
	for _, nodeSet := range nodeSets {
		nodeType := "cpu"
		if nodeSet.HasGPU {
			nodeType = "gpu"
		}
		parts = append(parts, fmt.Sprintf("%s=%d/%s", nodeSet.Name, nodeSet.Size, nodeType))
	}
	return strings.Join(parts, ", ")
}

func verifyPodReady(ctx context.Context, w *world, namespace, name string) error {
	output, err := w.Kubectl().RunWithDefaultRetry(ctx, "get", "pod", "-n", namespace, name, "-o", "json")
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
	state.WorkersByNodeSet = make(map[string][]framework.WorkerPodRef, len(state.DiscoveredNodeSets))
	state.CPUWorkers = nil
	state.GPUWorkers = nil

	if len(state.DiscoveredNodeSets) == 0 {
		return
	}

	discovered := slices.Clone(state.DiscoveredNodeSets)
	sort.Slice(discovered, func(i, j int) bool {
		return len(discovered[i].Name) > len(discovered[j].Name)
	})

	gpuByName := make(map[string]bool, len(discovered))
	for _, nodeSet := range discovered {
		gpuByName[nodeSet.Name] = nodeSet.HasGPU
	}

	for _, worker := range state.Workers {
		for _, nodeSet := range discovered {
			prefix := nodeSet.Name + "-"
			if !strings.HasPrefix(worker.Name, prefix) {
				continue
			}
			state.WorkersByNodeSet[nodeSet.Name] = append(state.WorkersByNodeSet[nodeSet.Name], worker)
			if gpuByName[nodeSet.Name] {
				state.GPUWorkers = append(state.GPUWorkers, worker)
			} else {
				state.CPUWorkers = append(state.CPUWorkers, worker)
			}
			break
		}
	}
}

func verifyDiscoveredWorkers(state *framework.ClusterState) error {
	var problems []string
	for _, nodeSet := range state.DiscoveredNodeSets {
		liveWorkers := len(state.WorkersByNodeSet[nodeSet.Name])
		if liveWorkers != nodeSet.Size {
			problems = append(problems, fmt.Sprintf("NodeSet %s live workers in Slurm=%d desired=%d", nodeSet.Name, liveWorkers, nodeSet.Size))
		}
	}

	desiredWorkers := state.DesiredWorkerCount()
	if desiredWorkers > 0 && len(state.Workers) != desiredWorkers {
		problems = append(problems, fmt.Sprintf("discovered workers=%d desired=%d", len(state.Workers), desiredWorkers))
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}
