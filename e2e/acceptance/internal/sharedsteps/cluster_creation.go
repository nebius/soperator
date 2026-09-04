package sharedsteps

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	clusterCreationHelmNamespace       = "flux-system"
	clusterCreationSmokeJobTimeout     = 2 * time.Minute
	clusterCreationPerNodeSmokeTimeout = 3 * time.Minute
	clusterCreationHCProgramTimeout    = 3 * time.Minute
)

var nodeStatePattern = regexp.MustCompile(`State=([^\s]+)`)

type ClusterCreation struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector
}

func NewClusterCreation(info *framework.ClusterInfo, runtime framework.Runtime, kubectl *framework.KubectlClient, selector *framework.WorkerSelector) *ClusterCreation {
	return &ClusterCreation{info: info, runtime: runtime, kubectl: kubectl, selector: selector}
}

func (s *ClusterCreation) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^all non-job pods in soperator are Running and Ready$`, s.checkPodsReady)
	sc.Step(`^all HelmReleases are Ready$`, s.checkHelmReleasesReady)
	sc.Step(`^all SlurmCluster CRs are available$`, s.checkSlurmClustersReady)
	sc.Step(`^all NodeSet CRs are ready$`, s.checkNodeSetsReady)
	sc.Step(`^main and hidden partitions are present and sane$`, s.checkPartitions)
	sc.Step(`^all Slurm nodes are healthy$`, s.checkSlurmNodeHealth)
	sc.Step(`^HealthCheckProgram outputs are healthy$`, s.checkHealthCheckProgramOutputs)
	sc.Step(`^no ActiveChecks are Failed or Error$`, s.checkActiveChecks)
	sc.Step(`^nebius user is present$`, s.checkNebiusUserPresent)
	sc.Step(`^soperatorchecks user is present and configured$`, s.checkSoperatorchecksUserPresentAndConfigured)
	sc.Step(`^login welcome output shows cluster information$`, s.checkWelcomeOutput)
	sc.Step(`^main partition smoke job succeeds$`, s.checkMainSmokeJob)
	sc.Step(`^hidden partition smoke job succeeds$`, s.checkHiddenSmokeJob)
	sc.Step(`^each discovered nodeset accepts a targeted smoke job$`, s.checkNodeSetSmokeJobs)
}

func (s *ClusterCreation) CleanupAndReset(ctx context.Context) {}

func (s *ClusterCreation) checkPodsReady(ctx context.Context) error {
	var pods corev1.PodList
	if err := s.kubectl.GetJSON(ctx, &pods, "get", "pods", "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return fmt.Errorf("list pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods found in namespace %s", framework.SoperatorNamespace)
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
		if !kubeobjects.PodReady(pod) {
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
	if err := s.kubectl.GetJSON(ctx, &releases, "get", "helmreleases", "-n", clusterCreationHelmNamespace, "-o", "json"); err != nil {
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
	var clusters kubeobjects.SlurmClusterList
	if err := s.kubectl.GetJSON(ctx, &clusters, "get", "slurmclusters", "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return fmt.Errorf("list SlurmClusters: %w", err)
	}
	if len(clusters.Items) == 0 {
		return fmt.Errorf("no SlurmClusters found")
	}

	var problems []string
	for _, cluster := range clusters.Items {
		if cluster.Status.Phase == nil || *cluster.Status.Phase != kubeobjects.SlurmClusterPhaseAvailable {
			phase := "<nil>"
			if cluster.Status.Phase != nil {
				phase = *cluster.Status.Phase
			}
			problems = append(problems, fmt.Sprintf("%s/%s phase=%s", cluster.Metadata.Namespace, cluster.Metadata.Name, phase))
			continue
		}

		required := []string{
			kubeobjects.SlurmClusterConditionControllersAvailable,
			kubeobjects.SlurmClusterConditionLoginAvailable,
			kubeobjects.SlurmClusterConditionSConfigControllerAvailable,
		}
		for _, conditionType := range required {
			condition := findMetaCondition(cluster.Status.Conditions, conditionType)
			if condition == nil || condition.Status != metav1.ConditionTrue {
				status := "<missing>"
				if condition != nil {
					status = string(condition.Status)
				}
				problems = append(problems, fmt.Sprintf("%s/%s %s=%s", cluster.Metadata.Namespace, cluster.Metadata.Name, conditionType, status))
			}
		}

		optional := []string{
			kubeobjects.SlurmClusterConditionCommonAvailable,
			kubeobjects.SlurmClusterConditionAccountingAvailable,
		}
		for _, conditionType := range optional {
			condition := findMetaCondition(cluster.Status.Conditions, conditionType)
			if condition != nil && condition.Status != metav1.ConditionTrue {
				problems = append(problems, fmt.Sprintf("%s/%s %s=%s", cluster.Metadata.Namespace, cluster.Metadata.Name, conditionType, condition.Status))
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
	nodeSets, err := s.kubectl.NodeSets(ctx, s.info.SlurmClusterName)
	if err != nil {
		return err
	}
	if len(nodeSets) == 0 {
		return fmt.Errorf("no NodeSets found")
	}

	var problems []string
	for _, nodeSet := range nodeSets {
		if nodeSet.Phase != kubeobjects.NodeSetPhaseReady {
			problems = append(problems, fmt.Sprintf("%s/%s phase=%s", nodeSet.Namespace, nodeSet.Name, nodeSet.Phase))
			continue
		}
		if nodeSet.ReadyReplicas != nodeSet.Replicas {
			problems = append(problems, fmt.Sprintf("%s/%s ready=%d desired=%d", nodeSet.Namespace, nodeSet.Name, nodeSet.ReadyReplicas, nodeSet.Replicas))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *ClusterCreation) checkPartitions(ctx context.Context) error {
	allPartitions, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "scontrol show partitions --oneliner")
	if err != nil {
		return fmt.Errorf("show partitions: %w", err)
	}
	if _, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "sinfo -Nel >/dev/null"); err != nil {
		return fmt.Errorf("sinfo -Nel: %w", err)
	}
	if !strings.Contains(allPartitions, "PartitionName=main") {
		return fmt.Errorf("partition main is missing from scontrol output")
	}
	if !strings.Contains(allPartitions, "PartitionName=hidden") {
		return fmt.Errorf("partition hidden is missing from scontrol output")
	}

	mainPartition, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "scontrol show partition main")
	if err != nil {
		return fmt.Errorf("show partition main: %w", err)
	}
	hiddenPartition, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "scontrol show partition hidden")
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
	nodesOutput, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "scontrol show nodes --oneliner")
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

func (s *ClusterCreation) checkHealthCheckProgramOutputs(ctx context.Context) error {
	if err := assertHealthCheckProgramConfigured(ctx, s.runtime); err != nil {
		return err
	}
	workers, err := s.selector.Workers(ctx)
	if err != nil {
		return err
	}
	if len(workers) == 0 {
		return fmt.Errorf("no workers found for HealthCheckProgram output validation")
	}

	var outputs map[string]string
	if err := s.runtime.WaitFor(ctx, "HealthCheckProgram outputs healthy", clusterCreationHCProgramTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		next := make(map[string]string, len(workers))
		for _, worker := range workers {
			output, exists, err := readWorkerFileIfExists(waitCtx, s.runtime, worker, checkRunnerOutputPath(s.info.TargetSoperatorVersion, worker.Name, "hc_program"))
			if err != nil {
				return false, err
			}
			if !exists {
				return false, nil
			}
			if err := assertCheckRunnerHealthy(output); err != nil {
				return false, err
			}
			next[worker.Name] = output
		}
		outputs = next
		return true, nil
	}); err != nil {
		return err
	}

	var problems []string
	for worker, output := range outputs {
		if err := assertLoggedCheckOutputsExist(ctx, s.runtime, framework.WorkerInfo{Name: worker}, output); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", worker, err))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *ClusterCreation) checkActiveChecks(ctx context.Context) error {
	var checks kubeobjects.ActiveCheckList
	if err := s.kubectl.GetJSON(ctx, &checks, "get", "activechecks", "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
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
			checkType = kubeobjects.ActiveCheckTypeK8sJob
		}

		switch checkType {
		case kubeobjects.ActiveCheckTypeK8sJob:
			if check.Status.K8sJobsStatus.LastJobStatus == kubeobjects.ActiveCheckK8sJobStatusFailed {
				problems = append(problems, fmt.Sprintf("%s/%s k8s status=%s", check.Metadata.Namespace, check.Metadata.Name, check.Status.K8sJobsStatus.LastJobStatus))
			}
		case kubeobjects.ActiveCheckTypeSlurmJob:
			status := check.Status.SlurmJobsStatus.LastRunStatus
			// InProgress is valid here: the HelmRelease hook already gated the initial run,
			// and this status can belong to a later scheduled execution.
			if status == kubeobjects.ActiveCheckSlurmRunStatusFailed || status == kubeobjects.ActiveCheckSlurmRunStatusError {
				problems = append(problems, fmt.Sprintf("%s/%s slurm status=%s", check.Metadata.Namespace, check.Metadata.Name, status))
			}
		default:
			problems = append(problems, fmt.Sprintf("%s/%s unknown checkType=%s", check.Metadata.Namespace, check.Metadata.Name, checkType))
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

func (s *ClusterCreation) checkNebiusUserPresent(ctx context.Context) error {
	return s.checkJailUserHome(ctx, "nebius")
}

func (s *ClusterCreation) checkSoperatorchecksUserPresentAndConfigured(ctx context.Context) error {
	if err := s.checkJailUserHome(ctx, "soperatorchecks"); err != nil {
		return err
	}

	key := "/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa"
	pub := key + ".pub"
	authorized := "/opt/soperator-home/soperatorchecks/.ssh/authorized_keys"
	for _, path := range []string{key, pub, authorized} {
		if _, err := s.runtime.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf("test -s %s", framework.ShellQuote(path))); err != nil {
			return fmt.Errorf("expected soperatorchecks SSH file %s to exist and be non-empty: %w", path, err)
		}
	}
	if _, err := s.runtime.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf("grep -Fxf %s %s >/dev/null",
		framework.ShellQuote(pub),
		framework.ShellQuote(authorized),
	)); err != nil {
		return fmt.Errorf("soperatorchecks public key is not present in authorized_keys: %w", err)
	}
	return nil
}

func (s *ClusterCreation) checkJailUserHome(ctx context.Context, user string) error {
	home := "/opt/soperator-home/" + user
	if _, err := s.runtime.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf("id %s >/dev/null && test -d %s",
		framework.ShellQuote(user),
		framework.ShellQuote(home),
	)); err != nil {
		return fmt.Errorf("check jail user %s with home %s: %w", user, home, err)
	}
	return nil
}

func (s *ClusterCreation) checkWelcomeOutput(ctx context.Context) error {
	output, err := s.runtime.Kubectl().RunWithDefaultRetry(ctx,
		"exec", "-n", framework.SoperatorNamespace, s.info.PodName("login-0"), "--", "sh", "-lc",
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
	output, err := s.runtime.Jail().Run(ctx, fmt.Sprintf("timeout %.0f srun -N 1 hostname", clusterCreationSmokeJobTimeout.Seconds()))
	if err != nil {
		return fmt.Errorf("run srun on default partition: %w", err)
	}
	if strings.TrimSpace(output) == "" {
		return fmt.Errorf("default partition smoke job returned empty output")
	}
	return nil
}

func (s *ClusterCreation) checkHiddenSmokeJob(ctx context.Context) error {
	output, err := s.runtime.Jail().Run(ctx, fmt.Sprintf("timeout %.0f srun -p hidden -N 1 hostname", clusterCreationSmokeJobTimeout.Seconds()))
	if err != nil {
		return fmt.Errorf("run srun on hidden partition: %w", err)
	}
	if strings.TrimSpace(output) == "" {
		return fmt.Errorf("hidden partition smoke job returned empty output")
	}
	return nil
}

func (s *ClusterCreation) checkNodeSetSmokeJobs(ctx context.Context) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	if len(snapshot.NodeSets) == 0 {
		s.runtime.Logf("cluster creation: no live nodesets configured, skipping per-nodeset smoke jobs")
		return nil
	}
	if err := snapshot.RequireAllWorkersUsable(); err != nil {
		return fmt.Errorf("validate workers for per-NodeSet smoke jobs: %w", err)
	}

	var problems []string
	for _, nodeSet := range snapshot.NodeSets {
		workers := append([]framework.WorkerInfo(nil), snapshot.WorkersByNodeSet[nodeSet.Name]...)
		if len(workers) == 0 {
			problems = append(problems, fmt.Sprintf("%s has no live workers", nodeSet.Name))
			continue
		}
		worker := workers[0]

		command := fmt.Sprintf("timeout %.0f srun -w %s hostname", clusterCreationPerNodeSmokeTimeout.Seconds(), framework.ShellQuote(worker.Name))
		if nodeSet.HasGPU {
			command = fmt.Sprintf("timeout %.0f srun -w %s nvidia-smi -L >/dev/null", clusterCreationPerNodeSmokeTimeout.Seconds(), framework.ShellQuote(worker.Name))
		}

		if _, err := s.runtime.Jail().Run(ctx, command); err != nil {
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
