package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	clusterCreationNamespace           = "soperator"
	clusterCreationHelmNamespace       = "flux-system"
	clusterCreationSmokeJobTimeout     = 2 * time.Minute
	clusterCreationPerNodeSmokeTimeout = 3 * time.Minute
)

var nodeStatePattern = regexp.MustCompile(`State=([^\s]+)`)

type ClusterCreation struct {
	state *framework.ClusterState
	exec  framework.Exec
}

func NewClusterCreation(state *framework.ClusterState, exec framework.Exec) *ClusterCreation {
	return &ClusterCreation{state: state, exec: exec}
}

func (s *ClusterCreation) Register(sc *godog.ScenarioContext) {
	sc.Step(`^all non-job pods in soperator are Running and Ready$`, s.checkPodsReady)
	sc.Step(`^all HelmReleases are Ready$`, s.checkHelmReleasesReady)
	sc.Step(`^all SlurmCluster CRs are available$`, s.checkSlurmClustersReady)
	sc.Step(`^all NodeSet CRs are ready$`, s.checkNodeSetsReady)
	sc.Step(`^configured nodesets match the live cluster$`, s.checkExpectedNodeSets)
	sc.Step(`^main and hidden partitions are present and sane$`, s.checkPartitions)
	sc.Step(`^all Slurm nodes are healthy$`, s.checkSlurmNodeHealth)
	sc.Step(`^all ActiveChecks completed successfully$`, s.checkActiveChecks)
	sc.Step(`^login welcome output shows cluster information$`, s.checkWelcomeOutput)
	sc.Step(`^main partition smoke job succeeds$`, s.checkMainSmokeJob)
	sc.Step(`^hidden partition smoke job succeeds$`, s.checkHiddenSmokeJob)
	sc.Step(`^each configured nodeset accepts a targeted smoke job$`, s.checkNodeSetSmokeJobs)
}

func (s *ClusterCreation) checkPodsReady(ctx context.Context) error {
	var pods corev1.PodList
	if err := kubectlJSON(ctx, s.exec, &pods, "get", "pods", "-n", clusterCreationNamespace, "-o", "json"); err != nil {
		return fmt.Errorf("list pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods found in namespace %s", clusterCreationNamespace)
	}

	var problems []string
	for _, pod := range pods.Items {
		if ownedByJob(pod) || pod.Status.Phase == corev1.PodSucceeded {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning {
			problems = append(problems, fmt.Sprintf("%s phase=%s", pod.Name, pod.Status.Phase))
			continue
		}
		if !podReady(pod) {
			problems = append(problems, fmt.Sprintf("%s not Ready", pod.Name))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *ClusterCreation) checkHelmReleasesReady(ctx context.Context) error {
	var releases helmReleaseList
	if err := kubectlJSON(ctx, s.exec, &releases, "get", "helmreleases", "-n", clusterCreationHelmNamespace, "-o", "json"); err != nil {
		return fmt.Errorf("list HelmReleases: %w", err)
	}
	if len(releases.Items) == 0 {
		return fmt.Errorf("no HelmReleases found in namespace %s", clusterCreationHelmNamespace)
	}

	var problems []string
	for _, release := range releases.Items {
		ready := findMetaCondition(release.Status.Conditions, "Ready")
		if ready == nil || ready.Status != metav1.ConditionTrue {
			status := "<missing>"
			if ready != nil {
				status = string(ready.Status)
			}
			problems = append(problems, fmt.Sprintf("%s Ready=%s", release.Metadata.Name, status))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *ClusterCreation) checkSlurmClustersReady(ctx context.Context) error {
	var clusters slurmv1.SlurmClusterList
	if err := kubectlJSON(ctx, s.exec, &clusters, "get", "slurmclusters", "-A", "-o", "json"); err != nil {
		return fmt.Errorf("list SlurmClusters: %w", err)
	}
	if len(clusters.Items) == 0 {
		return fmt.Errorf("no SlurmClusters found")
	}

	var problems []string
	for _, cluster := range clusters.Items {
		if cluster.Status.Phase == nil || *cluster.Status.Phase != slurmv1.PhaseClusterAvailable {
			phase := "<nil>"
			if cluster.Status.Phase != nil {
				phase = *cluster.Status.Phase
			}
			problems = append(problems, fmt.Sprintf("%s/%s phase=%s", cluster.Namespace, cluster.Name, phase))
			continue
		}

		required := []string{
			slurmv1.ConditionClusterControllersAvailable,
			slurmv1.ConditionClusterLoginAvailable,
			slurmv1.ConditionClusterSConfigControllerAvailable,
		}
		for _, conditionType := range required {
			condition := findMetaCondition(cluster.Status.Conditions, conditionType)
			if condition == nil || condition.Status != metav1.ConditionTrue {
				status := "<missing>"
				if condition != nil {
					status = string(condition.Status)
				}
				problems = append(problems, fmt.Sprintf("%s/%s %s=%s", cluster.Namespace, cluster.Name, conditionType, status))
			}
		}

		optional := []string{
			slurmv1.ConditionClusterCommonAvailable,
			slurmv1.ConditionClusterAccountingAvailable,
		}
		for _, conditionType := range optional {
			condition := findMetaCondition(cluster.Status.Conditions, conditionType)
			if condition != nil && condition.Status != metav1.ConditionTrue {
				problems = append(problems, fmt.Sprintf("%s/%s %s=%s", cluster.Namespace, cluster.Name, conditionType, condition.Status))
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *ClusterCreation) checkNodeSetsReady(ctx context.Context) error {
	var nodeSets slurmv1alpha1.NodeSetList
	if err := kubectlJSON(ctx, s.exec, &nodeSets, "get", "nodesets", "-A", "-o", "json"); err != nil {
		return fmt.Errorf("list NodeSets: %w", err)
	}
	if len(nodeSets.Items) == 0 {
		return fmt.Errorf("no NodeSets found")
	}

	var problems []string
	for _, nodeSet := range nodeSets.Items {
		if nodeSet.Status.Phase != slurmv1alpha1.PhaseNodeSetReady {
			problems = append(problems, fmt.Sprintf("%s/%s phase=%s", nodeSet.Namespace, nodeSet.Name, nodeSet.Status.Phase))
			continue
		}
		if nodeSet.Status.Replicas != nodeSet.Spec.Replicas {
			problems = append(problems, fmt.Sprintf("%s/%s ready=%d desired=%d", nodeSet.Namespace, nodeSet.Name, nodeSet.Status.Replicas, nodeSet.Spec.Replicas))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *ClusterCreation) checkExpectedNodeSets(ctx context.Context) error {
	if len(s.state.ExpectedNodeSets) == 0 {
		s.exec.Logf("cluster creation: no expected nodesets configured, skipping nodeset/profile comparison")
		return nil
	}

	var nodeSets slurmv1alpha1.NodeSetList
	if err := kubectlJSON(ctx, s.exec, &nodeSets, "get", "nodesets", "-A", "-o", "json"); err != nil {
		return fmt.Errorf("list NodeSets: %w", err)
	}

	actual := make(map[string]slurmv1alpha1.NodeSet, len(nodeSets.Items))
	for _, nodeSet := range nodeSets.Items {
		actual[nodeSet.Name] = nodeSet
	}

	var problems []string
	for _, expected := range s.state.ExpectedNodeSets {
		nodeSet, ok := actual[expected.Name]
		if !ok {
			problems = append(problems, fmt.Sprintf("NodeSet %s is missing", expected.Name))
			continue
		}
		if int(nodeSet.Spec.Replicas) != expected.Size {
			problems = append(problems, fmt.Sprintf("NodeSet %s desired=%d expected=%d", expected.Name, nodeSet.Spec.Replicas, expected.Size))
		}

		liveWorkers := len(s.state.WorkersByNodeSet[expected.Name])
		if liveWorkers != expected.Size {
			problems = append(problems, fmt.Sprintf("NodeSet %s live workers in Slurm=%d expected=%d", expected.Name, liveWorkers, expected.Size))
		}
	}

	expectedWorkers := s.state.ExpectedWorkerCount()
	if expectedWorkers > 0 && len(s.state.Workers) != expectedWorkers {
		problems = append(problems, fmt.Sprintf("discovered workers=%d expected=%d", len(s.state.Workers), expectedWorkers))
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *ClusterCreation) checkPartitions(ctx context.Context) error {
	allPartitions, err := s.exec.Controller().RunWithDefaultRetry(ctx, "scontrol show partitions --oneliner")
	if err != nil {
		return fmt.Errorf("show partitions: %w", err)
	}
	if _, err := s.exec.Controller().RunWithDefaultRetry(ctx, "sinfo -Nel >/dev/null"); err != nil {
		return fmt.Errorf("sinfo -Nel: %w", err)
	}
	if !strings.Contains(allPartitions, "PartitionName=main") {
		return fmt.Errorf("partition main is missing from scontrol output")
	}
	if !strings.Contains(allPartitions, "PartitionName=hidden") {
		return fmt.Errorf("partition hidden is missing from scontrol output")
	}

	mainPartition, err := s.exec.Controller().RunWithDefaultRetry(ctx, "scontrol show partition main")
	if err != nil {
		return fmt.Errorf("show partition main: %w", err)
	}
	hiddenPartition, err := s.exec.Controller().RunWithDefaultRetry(ctx, "scontrol show partition hidden")
	if err != nil {
		return fmt.Errorf("show partition hidden: %w", err)
	}

	var problems []string
	for _, expected := range []string{"Default=YES", "State=UP"} {
		if !strings.Contains(mainPartition, expected) {
			problems = append(problems, fmt.Sprintf("main missing %s", expected))
		}
	}
	for _, expected := range []string{"Hidden=YES", "State=UP"} {
		if !strings.Contains(hiddenPartition, expected) {
			problems = append(problems, fmt.Sprintf("hidden missing %s", expected))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *ClusterCreation) checkSlurmNodeHealth(ctx context.Context) error {
	nodesOutput, err := s.exec.Controller().RunWithDefaultRetry(ctx, "scontrol show nodes --oneliner")
	if err != nil {
		return fmt.Errorf("show nodes: %w", err)
	}

	var unhealthy []string
	for _, line := range strings.Split(nodesOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nodeName := extractField(line, "NodeName")
		state := extractNodeState(line)
		if nodeName == "" || state == "" {
			continue
		}

		for _, bad := range []string{"NOT_RESPONDING", "DOWN", "DRAIN", "FAIL", "INVALID_REG"} {
			if strings.Contains(state, bad) {
				unhealthy = append(unhealthy, fmt.Sprintf("%s state=%s", nodeName, state))
				break
			}
		}
	}

	if len(unhealthy) > 0 {
		sort.Strings(unhealthy)
		return fmt.Errorf("%s", strings.Join(unhealthy, "; "))
	}
	return nil
}

func (s *ClusterCreation) checkActiveChecks(ctx context.Context) error {
	var checks slurmv1alpha1.ActiveCheckList
	if err := kubectlJSON(ctx, s.exec, &checks, "get", "activechecks", "-A", "-o", "json"); err != nil {
		return fmt.Errorf("list ActiveChecks: %w", err)
	}
	if len(checks.Items) == 0 {
		return fmt.Errorf("no ActiveChecks found")
	}

	var problems []string
	checkedCount := 0
	for _, check := range checks.Items {
		runAfterCreation := true
		if check.Spec.RunAfterCreation != nil {
			runAfterCreation = *check.Spec.RunAfterCreation
		}
		if !runAfterCreation {
			continue
		}
		checkedCount++

		checkType := check.Spec.CheckType
		if checkType == "" {
			checkType = "k8sJob"
		}

		switch checkType {
		case "k8sJob":
			if check.Status.K8sJobsStatus.LastJobStatus != consts.ActiveCheckK8sJobStatusComplete {
				problems = append(problems, fmt.Sprintf("%s/%s k8s status=%s", check.Namespace, check.Name, check.Status.K8sJobsStatus.LastJobStatus))
			}
		case "slurmJob":
			if check.Status.SlurmJobsStatus.LastRunStatus != consts.ActiveCheckSlurmRunStatusComplete {
				problems = append(problems, fmt.Sprintf("%s/%s slurm status=%s", check.Namespace, check.Name, check.Status.SlurmJobsStatus.LastRunStatus))
			}
		default:
			problems = append(problems, fmt.Sprintf("%s/%s unknown checkType=%s", check.Namespace, check.Name, checkType))
		}
	}

	if checkedCount == 0 {
		return fmt.Errorf("no runAfterCreation ActiveChecks found")
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *ClusterCreation) checkWelcomeOutput(ctx context.Context) error {
	output, err := s.exec.RunWithDefaultRetry(ctx,
		"kubectl", "exec", "-n", clusterCreationNamespace, "login-0", "--", "sh", "-lc",
		"/etc/update-motd.d/00-welcome && /etc/update-motd.d/20-slurm-stats")
	if err != nil {
		return fmt.Errorf("render welcome output: %w", err)
	}

	var problems []string
	for _, expected := range []string{
		"Welcome to Soperator cluster",
		"Slurm nodes:",
		"main",
	} {
		if !strings.Contains(output, expected) {
			problems = append(problems, fmt.Sprintf("missing %q", expected))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *ClusterCreation) checkMainSmokeJob(ctx context.Context) error {
	output, err := s.exec.Jail().Run(ctx, fmt.Sprintf("timeout %.0f srun -N 1 hostname", clusterCreationSmokeJobTimeout.Seconds()))
	if err != nil {
		return fmt.Errorf("run srun on default partition: %w", err)
	}
	if strings.TrimSpace(output) == "" {
		return fmt.Errorf("default partition smoke job returned empty output")
	}
	return nil
}

func (s *ClusterCreation) checkHiddenSmokeJob(ctx context.Context) error {
	output, err := s.exec.Jail().Run(ctx, fmt.Sprintf("timeout %.0f srun -p hidden -N 1 hostname", clusterCreationSmokeJobTimeout.Seconds()))
	if err != nil {
		return fmt.Errorf("run srun on hidden partition: %w", err)
	}
	if strings.TrimSpace(output) == "" {
		return fmt.Errorf("hidden partition smoke job returned empty output")
	}
	return nil
}

func (s *ClusterCreation) checkNodeSetSmokeJobs(ctx context.Context) error {
	if len(s.state.ExpectedNodeSets) == 0 {
		s.exec.Logf("cluster creation: no expected nodesets configured, skipping per-nodeset smoke jobs")
		return nil
	}

	var problems []string
	for _, nodeSet := range s.state.ExpectedNodeSets {
		workers := slices.Clone(s.state.WorkersByNodeSet[nodeSet.Name])
		if len(workers) == 0 {
			problems = append(problems, fmt.Sprintf("%s has no discovered workers", nodeSet.Name))
			continue
		}
		worker := workers[0]

		command := fmt.Sprintf("timeout %.0f srun -w %s hostname", clusterCreationPerNodeSmokeTimeout.Seconds(), framework.ShellQuote(worker.Name))
		if nodeSet.HasGPU {
			command = fmt.Sprintf("timeout %.0f srun -w %s nvidia-smi -L >/dev/null", clusterCreationPerNodeSmokeTimeout.Seconds(), framework.ShellQuote(worker.Name))
		}

		if _, err := s.exec.Jail().Run(ctx, command); err != nil {
			problems = append(problems, fmt.Sprintf("%s worker %s smoke job failed: %v", nodeSet.Name, worker.Name, err))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

type helmReleaseList struct {
	Items []helmRelease `json:"items"`
}

type helmRelease struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Status helmReleaseStatusRef `json:"status"`
}

type helmReleaseStatusRef struct {
	Conditions []metav1.Condition `json:"conditions"`
}

func kubectlJSON(ctx context.Context, exec framework.Exec, out any, args ...string) error {
	output, err := exec.RunWithDefaultRetry(ctx, "kubectl", args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(output), out); err != nil {
		return fmt.Errorf("decode kubectl %s output: %w", strings.Join(args, " "), err)
	}
	return nil
}

func podReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func ownedByJob(pod corev1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "Job" {
			return true
		}
	}
	return false
}

func findMetaCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func extractField(line, field string) string {
	prefix := field + "="
	for _, token := range strings.Fields(line) {
		if strings.HasPrefix(token, prefix) {
			return strings.TrimPrefix(token, prefix)
		}
	}
	return ""
}

func extractNodeState(line string) string {
	match := nodeStatePattern.FindStringSubmatch(line)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}
