package sharedsteps

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	workerRolloutAnnotationKey  = "slurm.nebius.ai/e2e-rollout"
	workerRolloutJobName        = "e2e-worker-rolling-update"
	workerRolloutJobSleep       = 180 * time.Second
	workerRolloutIdleTimeout    = 4 * time.Minute
	workerRolloutDaemonSetWait  = 10 * time.Minute
	workerRolloutJobStartWait   = 5 * time.Minute
	workerRolloutJobFinishWait  = workerRolloutJobSleep + 4*time.Minute
	workerRolloutReplaceBase    = 3 * time.Minute
	workerRolloutReplacePerPod  = 6 * time.Minute
	workerRolloutStuckMargin    = 90 * time.Second
	workerRolloutSmokeTimeout   = 3 * time.Minute
	workerRolloutCleanupTimeout = 2 * time.Minute
)

type WorkerRollingUpdate struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	nodeSet         kubeobjects.NodeSet
	workers         []framework.WorkerInfo
	initialPods     map[string]workerPodSnapshot
	annotationValue string
	job             framework.SbatchJob
}

type workerPodSnapshot struct {
	UID       types.UID
	NodeName  string
	StartTime time.Time
}

func NewWorkerRollingUpdate(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient,
	selector *framework.WorkerSelector,
) *WorkerRollingUpdate {
	return &WorkerRollingUpdate{
		info:     info,
		runtime:  runtime,
		slurm:    slurm,
		kubectl:  kubectl,
		selector: selector,
	}
}

func (s *WorkerRollingUpdate) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a ready GPU NodeSet with slurm-aware rolling update is selected$`, s.selectNodeSet)
	sc.Step(`^the Soperator system components have been upgraded since the workers started$`, s.upgradeSystemComponents)
	sc.Step(`^a test job is running on one of its workers$`, s.submitJobOnWorker)
	sc.Step(`^the worker template of the NodeSet is changed$`, s.changeWorkerTemplate)
	sc.Step(`^the running job finishes without being killed$`, s.waitForJobCompleted)
	sc.Step(`^every worker pod of the NodeSet is replaced within the rollout budget$`, s.waitForWorkersReplaced)
	sc.Step(`^all workers of the NodeSet are idle with no leftover drain reason$`, s.waitForWorkersIdle)
	sc.Step(`^the updated NodeSet accepts a targeted smoke job$`, s.runSmokeJobs)
}

// CleanupAndReset only cancels a leftover job. The template annotation is kept on purpose: reverting it
// would start a second full rollout, and every run writes a fresh value anyway.
func (s *WorkerRollingUpdate) CleanupAndReset(ctx context.Context) {
	if !s.job.IsZero() {
		cleanupCtx, cancel := context.WithTimeout(ctx, workerRolloutCleanupTimeout)
		if err := s.slurm.CancelJob(cleanupCtx, s.job.ID, 0); err != nil {
			s.runtime.Logf("cleanup: cancel rolling update job %s: %v", s.job.ID, err)
		}
		cancel()
	}
	s.nodeSet = kubeobjects.NodeSet{}
	s.workers = nil
	s.initialPods = nil
	s.annotationValue = ""
	s.job = framework.SbatchJob{}
}

func (s *WorkerRollingUpdate) selectNodeSet(ctx context.Context) error {
	var nodeSets kubeobjects.NodeSetList
	if err := s.kubectl.GetJSON(ctx, &nodeSets, "get", "nodesets", "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return fmt.Errorf("list NodeSets: %w", err)
	}
	sort.Slice(nodeSets.Items, func(i, j int) bool {
		return nodeSets.Items[i].Metadata.Name < nodeSets.Items[j].Metadata.Name
	})

	var rejected []string
	for _, nodeSet := range nodeSets.Items {
		if reason := workerRolloutCandidateProblem(nodeSet, s.info.SlurmClusterName); reason != "" {
			rejected = append(rejected, fmt.Sprintf("%s: %s", nodeSet.Metadata.Name, reason))
			continue
		}
		s.nodeSet = nodeSet
		break
	}
	if s.nodeSet.Metadata.Name == "" {
		s.runtime.Logf("acceptance: no ready static GPU NodeSet with %s was found (%s), skipping scenario",
			kubeobjects.NodeSetUpdateStrategySlurmAwareRollingUpdate, strings.Join(rejected, "; "))
		return godog.ErrSkip
	}

	pods, err := s.nodeSetPods(ctx)
	if err != nil {
		return err
	}
	if int32(len(pods)) != s.nodeSet.Spec.Replicas {
		return fmt.Errorf("NodeSet %s has %d worker pods, expected %d", s.nodeSet.Metadata.Name, len(pods), s.nodeSet.Spec.Replicas)
	}
	s.initialPods = make(map[string]workerPodSnapshot, len(pods))
	for _, pod := range pods {
		if !kubeobjects.PodReady(pod) || pod.DeletionTimestamp != nil || pod.Status.StartTime == nil {
			return fmt.Errorf("worker pod %s is not a running ready pod: phase=%s deletionTimestamp=%v", pod.Name, pod.Status.Phase, pod.DeletionTimestamp)
		}
		s.initialPods[pod.Name] = workerPodSnapshot{UID: pod.UID, NodeName: pod.Spec.NodeName, StartTime: pod.Status.StartTime.Time}
	}

	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	s.workers = append([]framework.WorkerInfo(nil), snapshot.WorkersByNodeSet[s.nodeSet.Metadata.Name]...)
	sort.Slice(s.workers, func(i, j int) bool { return s.workers[i].Name < s.workers[j].Name })
	if int32(len(s.workers)) != s.nodeSet.Spec.Replicas {
		return fmt.Errorf("NodeSet %s has %d Slurm workers in the main partition, expected %d",
			s.nodeSet.Metadata.Name, len(s.workers), s.nodeSet.Spec.Replicas)
	}
	return s.waitForIdle(ctx, "selected NodeSet workers idle before the rollout")
}

// upgradeSystemComponents reproduces the part of a Soperator upgrade that precedes worker rollouts:
// the operator DaemonSets are rolled out, so their pods on worker nodes are younger than the workers.
func (s *WorkerRollingUpdate) upgradeSystemComponents(ctx context.Context) error {
	daemonSets, err := s.systemDaemonSets(ctx)
	if err != nil {
		return err
	}
	if len(daemonSets) == 0 {
		return fmt.Errorf("find DaemonSets in namespace %s", framework.SoperatorSystemNamespace)
	}
	previousGenerations := make(map[string]int64, len(daemonSets))
	for _, daemonSet := range daemonSets {
		previousGenerations[daemonSet.Name] = daemonSet.Generation
		if _, err := s.runtime.Kubectl().RunWithDefaultRetry(ctx,
			"rollout", "restart", "daemonset", daemonSet.Name, "-n", framework.SoperatorSystemNamespace); err != nil {
			return fmt.Errorf("restart DaemonSet %s: %w", daemonSet.Name, err)
		}
	}

	if err := s.runtime.WaitFor(ctx, "Soperator system DaemonSets rolled out", workerRolloutDaemonSetWait, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			current, err := s.systemDaemonSets(waitCtx)
			if err != nil {
				return false, err
			}
			var pending []string
			for _, daemonSet := range current {
				if !daemonSetRolledOut(daemonSet, previousGenerations[daemonSet.Name]) {
					pending = append(pending, fmt.Sprintf("%s updated=%d/%d available=%d",
						daemonSet.Name, daemonSet.Status.UpdatedNumberScheduled, daemonSet.Status.DesiredNumberScheduled, daemonSet.Status.NumberAvailable))
				}
			}
			if len(pending) > 0 {
				return false, fmt.Errorf("DaemonSets still rolling: %s", strings.Join(pending, "; "))
			}
			return true, nil
		}); err != nil {
		return err
	}

	// The scenario is only meaningful when a system pod was (re)started on each worker node after its worker.
	for podName, snapshot := range s.initialPods {
		var pods corev1.PodList
		if err := s.kubectl.GetJSON(ctx, &pods, "get", "pods", "-n", framework.SoperatorSystemNamespace,
			"--field-selector", "spec.nodeName="+snapshot.NodeName, "-o", "json"); err != nil {
			return fmt.Errorf("list system pods on node %s: %w", snapshot.NodeName, err)
		}
		if !daemonSetPodStartedAfter(snapshot.StartTime, pods.Items) {
			return fmt.Errorf("find a Soperator system DaemonSet pod on node %s started after worker pod %s (%s)",
				snapshot.NodeName, podName, snapshot.StartTime.Format(time.RFC3339))
		}
	}
	return nil
}

func (s *WorkerRollingUpdate) submitJobOnWorker(ctx context.Context) error {
	if len(s.workers) == 0 {
		return fmt.Errorf("select a NodeSet before submitting the job")
	}
	worker := s.workers[0]
	opts := framework.SbatchOptions{
		JobName:    workerRolloutJobName,
		ExtraFlags: []string{fmt.Sprintf("-w %s", framework.ShellQuote(worker.Name))},
		Wrap:       fmt.Sprintf("sleep %.0f", workerRolloutJobSleep.Seconds()),
	}
	if s.nodeSet.Spec.GPU.Enabled {
		opts.GPUsPerNode = 1
	}
	job, err := s.slurm.SubmitBatch(ctx, opts)
	if err != nil {
		return err
	}
	s.job = job
	s.runtime.Logf("worker rolling update: submitted job id=%s on %s stdout=%s", job.ID, worker.Name, job.StdoutPath)
	return s.slurm.WaitForJobRunning(ctx, job.ID, workerRolloutJobStartWait)
}

func (s *WorkerRollingUpdate) changeWorkerTemplate(ctx context.Context) error {
	s.annotationValue = time.Now().UTC().Format("20060102T150405Z")
	patch := fmt.Sprintf(`{"spec":{"workerAnnotations":{%q:%q}}}`, workerRolloutAnnotationKey, s.annotationValue)
	if _, err := s.runtime.Kubectl().RunWithDefaultRetry(ctx,
		"patch", "nodeset", s.nodeSet.Metadata.Name, "-n", s.nodeSet.Metadata.Namespace,
		"--type=merge", "-p", patch); err != nil {
		return fmt.Errorf("patch NodeSet worker annotations: %w", err)
	}
	return nil
}

func (s *WorkerRollingUpdate) waitForJobCompleted(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("submit the job before waiting for its completion")
	}
	var fatal error
	err := s.runtime.WaitFor(ctx, fmt.Sprintf("job %s completed", s.job.ID), workerRolloutJobFinishWait, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			info, err := s.slurm.JobInfo(waitCtx, s.job.ID)
			if err != nil {
				return false, err
			}
			done, failure := jobOutcome(info)
			if failure != "" {
				fatal = fmt.Errorf("%s", failure)
				return true, nil
			}
			if !done {
				return false, fmt.Errorf("job %s state=%q sacct=%q", s.job.ID, info.QueueState, info.SacctState)
			}
			return true, nil
		})
	if err == nil {
		err = fatal
	}
	if err != nil {
		return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.job, err)
	}
	s.job = framework.SbatchJob{}
	return nil
}

func (s *WorkerRollingUpdate) waitForWorkersReplaced(ctx context.Context) error {
	timeout := workerRolloutReplaceBase + time.Duration(s.nodeSet.Spec.Replicas)*workerRolloutReplacePerPod
	var fatal error
	var lastProgress string
	err := s.runtime.WaitFor(ctx, fmt.Sprintf("NodeSet %s worker pods replaced", s.nodeSet.Metadata.Name), timeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			pods, err := s.nodeSetPods(waitCtx)
			if err != nil {
				return false, err
			}
			now := time.Now()
			for _, pod := range pods {
				if past, terminatingFor := podTerminatingPastDeadline(pod, now); past {
					fatal = fmt.Errorf("worker pod %s on node %s is still terminating %s after its %ds grace period (phase=%s)",
						pod.Name, pod.Spec.NodeName, terminatingFor.Round(time.Second), podGracePeriodSeconds(pod), pod.Status.Phase)
					return true, nil
				}
			}
			remaining := replacedWorkerPods(s.nodeSet.Metadata.Name, s.initialPods, pods, s.nodeSet.Spec.Replicas, workerRolloutAnnotationKey, s.annotationValue)
			if len(remaining) == 0 {
				return true, nil
			}
			progress := strings.Join(remaining, ", ")
			if progress != lastProgress {
				s.runtime.Logf("worker rolling update: waiting for %s", progress)
				lastProgress = progress
			}
			return false, nil
		})
	if err != nil {
		return err
	}
	return fatal
}

func (s *WorkerRollingUpdate) waitForWorkersIdle(ctx context.Context) error {
	return s.waitForIdle(ctx, fmt.Sprintf("NodeSet %s workers idle after the rollout", s.nodeSet.Metadata.Name))
}

func (s *WorkerRollingUpdate) runSmokeJobs(ctx context.Context) error {
	var problems []string
	for _, worker := range s.workers {
		command := fmt.Sprintf("timeout %.0f srun -w %s hostname", workerRolloutSmokeTimeout.Seconds(), framework.ShellQuote(worker.Name))
		if s.nodeSet.Spec.GPU.Enabled {
			command = fmt.Sprintf("timeout %.0f srun -w %s --gpus-per-node=1 nvidia-smi -L >/dev/null",
				workerRolloutSmokeTimeout.Seconds(), framework.ShellQuote(worker.Name))
		}
		jobCtx, cancel := context.WithTimeout(ctx, workerRolloutSmokeTimeout+time.Minute)
		_, err := s.runtime.Jail().Run(jobCtx, command)
		cancel()
		if err != nil {
			problems = append(problems, fmt.Sprintf("worker %s smoke job failed: %v", worker.Name, err))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *WorkerRollingUpdate) waitForIdle(ctx context.Context, description string) error {
	return s.runtime.WaitFor(ctx, description, workerRolloutIdleTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			nodes := make([]framework.SlurmNodeInfo, 0, len(s.workers))
			for _, worker := range s.workers {
				node, err := s.slurm.NodeInfo(waitCtx, worker.Name)
				if err != nil {
					return false, err
				}
				nodes = append(nodes, node)
			}
			if problems := idleProblems(nodes); len(problems) > 0 {
				return false, fmt.Errorf("%s", strings.Join(problems, "; "))
			}
			return true, nil
		})
}

func (s *WorkerRollingUpdate) nodeSetPods(ctx context.Context) ([]corev1.Pod, error) {
	var pods corev1.PodList
	selector := fmt.Sprintf("%s=%s", nodeSetLabelKey, s.nodeSet.Metadata.Name)
	if err := s.kubectl.GetJSON(ctx, &pods, "get", "pods", "-n", s.nodeSet.Metadata.Namespace,
		"-l", selector, "-o", "json"); err != nil {
		return nil, fmt.Errorf("list worker pods for NodeSet %s: %w", s.nodeSet.Metadata.Name, err)
	}
	return pods.Items, nil
}

func (s *WorkerRollingUpdate) systemDaemonSets(ctx context.Context) ([]appsv1.DaemonSet, error) {
	var daemonSets appsv1.DaemonSetList
	if err := s.kubectl.GetJSON(ctx, &daemonSets, "get", "daemonsets", "-n", framework.SoperatorSystemNamespace, "-o", "json"); err != nil {
		return nil, fmt.Errorf("list DaemonSets in %s: %w", framework.SoperatorSystemNamespace, err)
	}
	return daemonSets.Items, nil
}

// workerRolloutCandidateProblem explains why a NodeSet cannot drive the scenario, or returns "" for a fit.
func workerRolloutCandidateProblem(nodeSet kubeobjects.NodeSet, clusterName string) string {
	switch {
	case nodeSet.Spec.ClusterName != "" && nodeSet.Spec.ClusterName != clusterName:
		return "belongs to another cluster"
	case !nodeSet.Spec.GPU.Enabled:
		return "no GPU"
	case nodeSet.Spec.UpdateStrategy != kubeobjects.NodeSetUpdateStrategySlurmAwareRollingUpdate:
		return fmt.Sprintf("updateStrategy=%q", nodeSet.Spec.UpdateStrategy)
	case nodeSet.Spec.EphemeralNodes != nil && *nodeSet.Spec.EphemeralNodes:
		return "ephemeral mode"
	case nodeSet.Spec.Replicas == 0:
		return "no replicas"
	case nodeSet.Status.Replicas != nodeSet.Spec.Replicas || nodeSet.Status.Phase != kubeobjects.NodeSetPhaseReady:
		return fmt.Sprintf("phase=%s ready=%d/%d", nodeSet.Status.Phase, nodeSet.Status.Replicas, nodeSet.Spec.Replicas)
	}
	return ""
}

// podGracePeriodSeconds is set by the API server together with deletionTimestamp.
func podGracePeriodSeconds(pod corev1.Pod) int64 {
	if pod.DeletionGracePeriodSeconds != nil {
		return *pod.DeletionGracePeriodSeconds
	}
	return 0
}

// podTerminatingPastDeadline reports whether a pod has been terminating longer than its grace period plus the stuck margin.
func podTerminatingPastDeadline(pod corev1.Pod, now time.Time) (past bool, terminatingFor time.Duration) {
	if pod.DeletionTimestamp == nil {
		return false, 0
	}
	terminatingFor = now.Sub(pod.DeletionTimestamp.Time)
	deadline := time.Duration(podGracePeriodSeconds(pod))*time.Second + workerRolloutStuckMargin
	return terminatingFor > deadline, terminatingFor
}

// replacedWorkerPods lists the ordinals that are not yet replaced by a ready pod carrying the new template.
func replacedWorkerPods(nodeSetName string, initial map[string]workerPodSnapshot, pods []corev1.Pod, replicas int32, annotationKey, annotationValue string) (remaining []string) {
	byName := make(map[string]corev1.Pod, len(pods))
	for _, pod := range pods {
		byName[pod.Name] = pod
	}
	for ordinal := range replicas {
		name := fmt.Sprintf("%s-%d", nodeSetName, ordinal)
		pod, ok := byName[name]
		switch {
		case !ok:
			remaining = append(remaining, name+" (missing)")
		case pod.UID == initial[name].UID:
			remaining = append(remaining, name+" (old pod)")
		case pod.DeletionTimestamp != nil:
			remaining = append(remaining, name+" (terminating)")
		case pod.Annotations[annotationKey] != annotationValue:
			remaining = append(remaining, name+" (old template)")
		case !kubeobjects.PodReady(pod):
			remaining = append(remaining, fmt.Sprintf("%s (%s, not ready)", name, pod.Status.Phase))
		}
	}
	return remaining
}

// daemonSetRolledOut requires a generation bump past previousGeneration so a stale status cannot pass.
func daemonSetRolledOut(daemonSet appsv1.DaemonSet, previousGeneration int64) bool {
	status := daemonSet.Status
	return daemonSet.Generation > previousGeneration &&
		status.ObservedGeneration >= daemonSet.Generation &&
		status.UpdatedNumberScheduled == status.DesiredNumberScheduled &&
		status.NumberAvailable == status.DesiredNumberScheduled &&
		status.NumberUnavailable == 0
}

func daemonSetPodStartedAfter(workerStart time.Time, pods []corev1.Pod) bool {
	for _, pod := range pods {
		if !ownedByDaemonSet(pod) || pod.Status.Phase != corev1.PodRunning || pod.Status.StartTime == nil {
			continue
		}
		if pod.Status.StartTime.Time.After(workerStart) {
			return true
		}
	}
	return false
}

func ownedByDaemonSet(pod corev1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

func idleProblems(nodes []framework.SlurmNodeInfo) []string {
	var problems []string
	for _, node := range nodes {
		if !node.HasStateFlag("IDLE") || !node.IsUsable() || node.Reason != "" {
			problems = append(problems, fmt.Sprintf("%s state=%s reason=%q", node.Name, node.State, node.Reason))
		}
	}
	return problems
}

// jobOutcome reports whether the job reached a terminal state; a non-empty failure means it did not complete cleanly.
func jobOutcome(info framework.SlurmJobInfo) (done bool, failure string) {
	if info.IsAlive() || !info.SacctFound {
		return false, ""
	}
	if !info.CompletedSuccessfully() {
		return true, fmt.Sprintf("job %s ended in state %s with exit code %s", info.ID, info.SacctState, info.SacctExit)
	}
	return true, ""
}
