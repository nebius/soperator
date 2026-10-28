package sharedsteps

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	workerRolloutAnnotationKey  = "slurm.nebius.ai/e2e-rollout"
	workerRolloutJobName        = "e2e-worker-rolling-update"
	workerRolloutDrainTimeout   = 5 * time.Minute
	workerRolloutIdleTimeout    = 4 * time.Minute
	workerRolloutDaemonSetWait  = 10 * time.Minute
	workerRolloutJobStartWait   = 5 * time.Minute
	workerRolloutJobFinishWait  = 5 * time.Minute
	workerRolloutReplaceBase    = 3 * time.Minute
	workerRolloutReplacePerPod  = 6 * time.Minute
	workerRolloutCleanupTimeout = 2 * time.Minute
)

type WorkerRollingUpdate struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	nodeSet             kubeobjects.NodeSet
	workerNames         []string
	workers             []framework.WorkerInfo
	initialPods         map[string]workerPodSnapshot
	annotationValue     string
	restartedDaemonSets []string
	targets             []*workerRolloutTarget
	mk8s                workerRolloutMK8s
	power               workerRolloutPower
	requireMK8s         bool
}

type workerPodSnapshot struct {
	UID      types.UID
	NodeName string
}

type workerRolloutTarget struct {
	worker      framework.WorkerInfo
	replacement *NodeReplacement
	job         workerRolloutJob
}

type workerRolloutJob struct {
	job         framework.SbatchJob
	releasePath string
}

type workerRolloutPrerequisiteError struct{ error }

type rolloutInvariantError struct{ error }

func (e rolloutInvariantError) Unwrap() error { return e.error }

// Exclusions protect the selected active workers without running extra jobs that
// would block their handoff. Only additions owned by this scenario are removed.
type workerRolloutPower struct {
	names []string
	added []string
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
	sc.Step(`^test jobs are running on two workers of a NodeSet with slurmAwareRollingUpdate strategy and maxUnavailable at least 2$`, s.prepareRolloutWorkers)
	sc.Step(`^the Soperator system DaemonSets are restarted$`, s.restartSystemDaemonSets)
	sc.Step(`^the worker template of the NodeSet is changed$`, s.changeWorkerTemplate)
	sc.Step(`^all test jobs finish without interruption$`, s.finishRolloutJobs)
	sc.Step(`^all active workers of the NodeSet are idle$`, s.waitForWorkersIdle)
	sc.Step(`^the updated NodeSet accepts smoke jobs$`, s.runRolloutSmokeJobs)
	s.registerMaintenanceSteps(sc)
	s.registerMK8sSteps(sc)
}

// CleanupAndReset stops temporary jobs and waits for accepted rollouts and
// maintenance to leave healthy workers before releasing suspension holds.
// Worker template annotations are retained; MK8s node-group changes are restored
// before releasing the suspension holds.
func (s *WorkerRollingUpdate) CleanupAndReset(ctx context.Context) {
	// Cancel every held job before waiting for either host's maintenance.
	for _, target := range s.targets {
		s.cleanupWorkerRolloutJob(ctx, target.job)
	}
	for _, target := range s.targets {
		s.waitForWorkerRolloutJobGone(ctx, target.job.job)
	}
	for _, target := range s.targets {
		if target.replacement != nil {
			target.replacement.CleanupAndReset(ctx)
		}
	}
	s.cleanupSystemDaemonSets(ctx)
	if s.mk8s.requestedMaxPods != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*workerMK8sTimeout)
		if err := s.cleanupMK8sGroup(cleanupCtx); err != nil {
			s.runtime.Logf("Cleanup: restore MK8s node group %s: %v", s.mk8s.groupID, err)
		}
		cancel()
	}
	if s.nodeSet.Metadata.Name != "" {
		s.cleanupRollout(ctx)
	}
	s.targets = nil
	s.cleanupEphemeralWorkerHold(ctx)

	s.mk8s = workerRolloutMK8s{}
	s.nodeSet = kubeobjects.NodeSet{}
	s.workerNames = nil
	s.workers = nil
	s.initialPods = nil
	s.annotationValue = ""
	s.restartedDaemonSets = nil
	s.requireMK8s = false
}

func (s *WorkerRollingUpdate) cleanupSystemDaemonSets(ctx context.Context) {
	if len(s.restartedDaemonSets) == 0 {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workerRolloutDaemonSetWait)
	defer cancel()
	err := s.runtime.WaitFor(cleanupCtx, "restarted Soperator system DaemonSets healthy after cleanup",
		workerRolloutDaemonSetWait, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			current, err := s.systemDaemonSets(waitCtx)
			if err != nil {
				return false, err
			}
			byName := make(map[string]appsv1.DaemonSet, len(current))
			for _, daemonSet := range current {
				byName[daemonSet.Name] = daemonSet
			}
			for _, name := range s.restartedDaemonSets {
				daemonSet, found := byName[name]
				if !found {
					return false, fmt.Errorf("restarted DaemonSet %s/%s is missing", framework.SoperatorSystemNamespace, name)
				}
				if !daemonSetConverged(daemonSet) {
					return false, fmt.Errorf("DaemonSet %s/%s generation=%d observed=%d updated=%d/%d available=%d unavailable=%d terminating=%t",
						framework.SoperatorSystemNamespace, name, daemonSet.Generation, daemonSet.Status.ObservedGeneration,
						daemonSet.Status.UpdatedNumberScheduled, daemonSet.Status.DesiredNumberScheduled, daemonSet.Status.NumberAvailable,
						daemonSet.Status.NumberUnavailable, daemonSet.DeletionTimestamp != nil)
				}
			}
			return true, nil
		})
	if err != nil {
		s.runtime.Logf("Cleanup: wait for restarted Soperator system DaemonSets %v to become healthy: %v", s.restartedDaemonSets, err)
	}
}

func (s *WorkerRollingUpdate) waitForWorkerRolloutJobGone(ctx context.Context, job framework.SbatchJob) {
	if job.IsZero() && job.JobName == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workerRolloutCleanupTimeout)
	defer cancel()
	var err error
	if !job.IsZero() {
		err = s.slurm.WaitForJobGone(cleanupCtx, job.ID, workerRolloutCleanupTimeout)
	} else {
		err = s.runtime.WaitFor(cleanupCtx, "rollout job named "+job.JobName+" gone from queue",
			workerRolloutCleanupTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
				output, queryErr := s.runtime.Jail().RunWithDefaultRetry(waitCtx,
					"squeue -h --name="+framework.ShellQuote(job.JobName)+" -o '%i'")
				if queryErr != nil {
					return false, queryErr
				}
				if pending := strings.TrimSpace(output); pending != "" {
					return false, fmt.Errorf("rollout job named %s still queued: %s", job.JobName, pending)
				}
				return true, nil
			})
	}
	if err != nil {
		s.runtime.Logf("Cleanup: wait for rollout job ID=%s name=%s to leave the queue: %v", job.ID, job.JobName, err)
	}
}

func (s *WorkerRollingUpdate) cleanupRollout(ctx context.Context) {
	timeout := workerRolloutReplaceBase + time.Duration(len(s.initialPods))*workerRolloutReplacePerPod + workerRolloutIdleTimeout
	if s.mk8s.requestedMaxPods != nil {
		timeout = workerMK8sTimeout
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	var lastConfiguration string
	err := s.runtime.WaitFor(cleanupCtx, "NodeSet "+s.nodeSet.Metadata.Name+" healthy after rollout cleanup",
		timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			var current kubeobjects.NodeSet
			if err := s.kubectl.GetJSON(waitCtx, &current, "get", "nodeset", s.nodeSet.Metadata.Name,
				"-n", s.nodeSet.Metadata.Namespace, "-o", "json"); err != nil {
				return false, err
			}
			names, err := s.rolloutWorkerNames(waitCtx, current)
			if err != nil {
				return false, err
			}
			if current.Status.Phase != kubeobjects.NodeSetPhaseReady || int(current.Status.Replicas) != len(names) {
				return false, fmt.Errorf("NodeSet %s phase=%s ready=%d active=%d", current.Metadata.Name, current.Status.Phase, current.Status.Replicas, len(names))
			}
			pods, err := nodeSetWorkerPods(waitCtx, s.kubectl, current)
			if err != nil {
				return false, err
			}
			if err := requireRolloutPodNames(names, pods); err != nil {
				return false, err
			}
			annotation := current.Spec.WorkerAnnotations[workerRolloutAnnotationKey]
			if s.annotationValue != "" && annotation != s.annotationValue &&
				annotation != s.nodeSet.Spec.WorkerAnnotations[workerRolloutAnnotationKey] && annotation != lastConfiguration {
				s.runtime.Logf("Cleanup: NodeSet %s rollout annotation changed externally; waiting for its current template without overwriting it", current.Metadata.Name)
				lastConfiguration = annotation
			}
			for _, pod := range pods {
				if pod.DeletionTimestamp != nil || pod.Spec.NodeName == "" || !kubeobjects.PodReady(pod) || pod.Annotations[workerRolloutAnnotationKey] != annotation {
					return false, fmt.Errorf("worker %s not converged: host=%s phase=%s ready=%t terminating=%t current_template=%t",
						pod.Name, pod.Spec.NodeName, pod.Status.Phase, kubeobjects.PodReady(pod), pod.DeletionTimestamp != nil, pod.Annotations[workerRolloutAnnotationKey] == annotation)
				}
			}
			var nodes corev1.NodeList
			if err := s.kubectl.GetJSON(waitCtx, &nodes, "get", "nodes", "-o", "json"); err != nil {
				return false, err
			}
			hosts := make(map[string]corev1.Node, len(nodes.Items))
			for _, node := range nodes.Items {
				hosts[node.Name] = node
			}
			for _, pod := range pods {
				host, found := hosts[pod.Spec.NodeName]
				if !found || host.DeletionTimestamp != nil || host.Spec.Unschedulable || !k8sNodeReady(host) {
					return false, fmt.Errorf("worker %s host %s not ready and schedulable", pod.Name, pod.Spec.NodeName)
				}
			}
			if s.mk8s.requestedMaxPods != nil {
				if err := s.mk8sGroupAndHostsReady(waitCtx, len(names), nodes.Items, true); err != nil {
					return false, err
				}
			}
			if len(names) == 0 {
				return true, nil
			}
			workers, err := s.slurm.NodeInfosOnce(waitCtx, names)
			if err != nil {
				return false, err
			}
			if problems := idleProblems(workers); len(problems) != 0 {
				return false, fmt.Errorf("%s", strings.Join(problems, "; "))
			}
			return true, nil
		})
	if err != nil {
		s.runtime.Logf("Cleanup: wait for NodeSet %s/%s and MK8s group %s to reach a healthy baseline (retaining accepted changes): %v",
			s.nodeSet.Metadata.Namespace, s.nodeSet.Metadata.Name, s.mk8s.groupID, err)
	}
}

func (s *WorkerRollingUpdate) cleanupWorkerRolloutJob(ctx context.Context, owned workerRolloutJob) {
	if owned.job.IsZero() && owned.job.JobName == "" && owned.releasePath == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workerRolloutCleanupTimeout)
	defer cancel()
	if !owned.job.IsZero() {
		if err := s.slurm.CancelJob(cleanupCtx, owned.job.ID, 0); err != nil {
			s.runtime.Logf("Cleanup: cancel rolling update job %s: %v", owned.job.ID, err)
		}
	} else if owned.job.JobName != "" {
		// Submission may succeed even if its response (and therefore job ID) is lost.
		if _, err := s.runtime.Controller().Run(cleanupCtx, "scancel --name="+framework.ShellQuote(owned.job.JobName)); err != nil {
			s.runtime.Logf("Cleanup: cancel rollout job named %s: %v", owned.job.JobName, err)
		}
	}
	if owned.releasePath != "" {
		if _, err := s.runtime.Jail().Run(cleanupCtx, "rm -f -- "+framework.ShellQuote(owned.releasePath)); err != nil {
			s.runtime.Logf("Cleanup: remove rollout gate %s: %v", owned.releasePath, err)
		}
	}
}

func (s *WorkerRollingUpdate) prepareRolloutWorkers(ctx context.Context) error {
	if err := s.selectNodeSet(ctx); err != nil {
		return err
	}
	for _, worker := range s.workers[:2] {
		s.targets = append(s.targets, &workerRolloutTarget{worker: worker})
	}
	for _, target := range s.targets {
		owned, err := s.submitRolloutJob(ctx, target.worker, workerRolloutJobGateWait())
		target.job = owned
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *WorkerRollingUpdate) selectNodeSet(ctx context.Context) error {
	nodeSets, err := listClusterNodeSets(ctx, s.kubectl, s.info.SlurmClusterName)
	if err != nil {
		return err
	}

	var rejected []string
	for _, nodeSet := range nodeSets {
		if reason := workerRolloutConfigurationProblem(nodeSet, s.info.SlurmClusterName); reason != "" {
			rejected = append(rejected, fmt.Sprintf("%s: %s", nodeSet.Metadata.Name, reason))
			continue
		}
		names, err := s.rolloutWorkerNames(ctx, nodeSet)
		if err != nil {
			return err
		}
		if reason := workerRolloutCandidateProblem(nodeSet, s.info.SlurmClusterName, len(names)); reason != "" {
			return fmt.Errorf("require configured rollout NodeSet %s to be healthy: %s", nodeSet.Metadata.Name, reason)
		}
		if len(names) < 2 {
			rejected = append(rejected, fmt.Sprintf("%s: %d active workers, require at least %d", nodeSet.Metadata.Name, len(names), 2))
			continue
		}
		budget, err := workerRolloutBudget(nodeSet.Spec.MaxUnavailable, len(names))
		if err != nil {
			return fmt.Errorf("read NodeSet %s maxUnavailable: %w", nodeSet.Metadata.Name, err)
		}
		if budget < 2 {
			rejected = append(rejected, fmt.Sprintf("%s: effective maxUnavailable=%d, require at least 2", nodeSet.Metadata.Name, budget))
			continue
		}
		candidate := *s
		candidate.nodeSet = nodeSet
		candidate.workerNames = names
		candidate.mk8s = workerRolloutMK8s{}
		if err := candidate.snapshotSelectedWorkers(ctx); err != nil {
			return err
		}
		hosts := make(map[string]bool)
		for _, pod := range candidate.initialPods {
			hosts[pod.NodeName] = true
		}
		if len(hosts) != len(candidate.initialPods) {
			rejected = append(rejected, fmt.Sprintf("%s: require a separate host for each worker", nodeSet.Metadata.Name))
			continue
		}
		if s.requireMK8s {
			if err := candidate.selectMK8sGroup(ctx); err != nil {
				var prerequisite workerRolloutPrerequisiteError
				if !errors.As(err, &prerequisite) {
					return err
				}
				rejected = append(rejected, fmt.Sprintf("%s: %v", nodeSet.Metadata.Name, err))
				continue
			}
		}
		*s = candidate
		if isEphemeralRollout(s.nodeSet) {
			if err := s.holdEphemeralWorkers(ctx, s.workerNames); err != nil {
				return err
			}
			if err := s.checkRolloutWorkerSet(ctx); err != nil {
				return err
			}
			// Record the baseline after active ephemeral workers are protected.
			if err := s.snapshotSelectedWorkers(ctx); err != nil {
				return err
			}
		}
		return s.waitForIdle(ctx, "selected NodeSet workers idle before the rollout", workerRolloutIdleTimeout)
	}
	s.runtime.Logf("Acceptance: no NodeSet satisfies rollout prerequisites (%s), skipping scenario", strings.Join(rejected, "; "))
	return godog.ErrSkip
}

func (s *WorkerRollingUpdate) snapshotSelectedWorkers(ctx context.Context) error {
	pods, err := nodeSetWorkerPods(ctx, s.kubectl, s.nodeSet)
	if err != nil {
		return err
	}
	if err := requireRolloutPodNames(s.workerNames, pods); err != nil {
		return fmt.Errorf("check NodeSet %s active pods: %w", s.nodeSet.Metadata.Name, err)
	}
	s.initialPods = make(map[string]workerPodSnapshot, len(pods))
	for _, pod := range pods {
		if !kubeobjects.PodReady(pod) || pod.DeletionTimestamp != nil || pod.UID == "" || pod.Spec.NodeName == "" {
			return fmt.Errorf("worker pod %s is not a running ready pod: phase=%s deletionTimestamp=%v", pod.Name, pod.Status.Phase, pod.DeletionTimestamp)
		}
		snapshot := workerPodSnapshot{UID: pod.UID, NodeName: pod.Spec.NodeName}
		s.initialPods[pod.Name] = snapshot
	}

	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	pool := snapshot.WorkersByNodeSet[s.nodeSet.Metadata.Name]
	if !isEphemeralRollout(s.nodeSet) && len(pool) != len(s.workerNames) {
		return fmt.Errorf("NodeSet %s has %d Slurm workers in the main partition, expected %d", s.nodeSet.Metadata.Name, len(pool), len(s.workerNames))
	}
	s.workers, err = selectRolloutWorkers(s.workerNames, pool)
	if err != nil {
		return fmt.Errorf("select NodeSet %s active Slurm workers: %w", s.nodeSet.Metadata.Name, err)
	}
	return nil
}

// restartSystemDaemonSets reproduces the upgrade ordering where system pods restart before workers.
func (s *WorkerRollingUpdate) restartSystemDaemonSets(ctx context.Context) error {
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
		s.restartedDaemonSets = append(s.restartedDaemonSets, daemonSet.Name)
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

	return nil
}

func (s *WorkerRollingUpdate) submitRolloutJob(ctx context.Context, worker framework.WorkerInfo, gateWait time.Duration) (workerRolloutJob, error) {
	if err := s.checkRolloutWorkerSet(ctx); err != nil {
		return workerRolloutJob{}, err
	}
	gateDir := framework.AcceptanceJobOutputDir
	if artifacts, ok := framework.ScenarioArtifacts(ctx); ok {
		gateDir = artifacts.JailDir
	}
	owned := workerRolloutJob{
		job:         framework.SbatchJob{JobName: fmt.Sprintf("%s-%d", workerRolloutJobName, time.Now().UnixNano())},
		releasePath: path.Join(gateDir, fmt.Sprintf("worker-rollout-%d.release", time.Now().UnixNano())),
	}
	timeLimit := gateWait + workerRolloutJobFinishWait
	opts := framework.SbatchOptions{
		JobName:    owned.job.JobName,
		Nodelist:   []string{worker.Name},
		Nodes:      1,
		ExtraFlags: []string{"--no-requeue", fmt.Sprintf("--time=%.0f", timeLimit.Minutes()), "--cpus-per-task=1", "--mem=128M"},
		Wrap:       framework.JobReleaseGate(owned.releasePath, gateWait),
	}
	if s.nodeSet.Spec.GPU.Enabled {
		opts.GPUsPerNode = 1
	}
	job, err := s.slurm.SubmitBatch(ctx, opts)
	if err != nil {
		return owned, err
	}
	owned.job = job
	s.runtime.Logf("Worker rolling update: submitted job id=%s on %s stdout=%s", job.ID, worker.Name, job.StdoutPath)
	return owned, s.slurm.WaitForJobRunning(ctx, job.ID, workerRolloutJobStartWait)
}

func (s *WorkerRollingUpdate) changeWorkerTemplate(ctx context.Context) error {
	if err := s.checkRolloutWorkerSet(ctx); err != nil {
		return err
	}
	s.annotationValue = fmt.Sprintf("%d", time.Now().UnixNano())
	patch := fmt.Sprintf(`{"spec":{"workerAnnotations":{%q:%q}}}`, workerRolloutAnnotationKey, s.annotationValue)
	if _, err := s.runtime.Kubectl().RunWithDefaultRetry(ctx,
		"patch", "nodeset", s.nodeSet.Metadata.Name, "-n", s.nodeSet.Metadata.Namespace,
		"--type=merge", "-p", patch); err != nil {
		return fmt.Errorf("patch NodeSet worker annotations: %w", err)
	}
	return nil
}

func (s *WorkerRollingUpdate) releaseWorkerRolloutJob(ctx context.Context, owned workerRolloutJob) error {
	if owned.job.IsZero() || owned.releasePath == "" {
		return fmt.Errorf("submit the job before waiting for its completion")
	}
	if _, err := s.runtime.Jail().Run(ctx, "touch -- "+framework.ShellQuote(owned.releasePath)); err != nil {
		return fmt.Errorf("release rollout job %s: %w", owned.job.ID, err)
	}
	return nil
}

func (s *WorkerRollingUpdate) finishRolloutJobs(ctx context.Context) error {
	if len(s.targets) != 2 {
		return fmt.Errorf("start jobs on two workers before completing the rollout")
	}
	for _, target := range s.targets {
		if err := s.releaseWorkerRolloutJob(ctx, target.job); err != nil {
			return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, target.job.job, err)
		}
	}
	for _, target := range s.targets {
		if err := framework.WaitForJobSucceeded(ctx, s.runtime, s.slurm, target.job.job, workerRolloutJobFinishWait); err != nil {
			return err
		}
	}
	return nil
}

func (s *WorkerRollingUpdate) runRolloutSmokeJobs(ctx context.Context) error {
	if len(s.workers) == 0 {
		return fmt.Errorf("run smoke jobs: no active workers were selected")
	}
	validationCtx, cancel := context.WithTimeout(ctx, time.Duration(len(s.workers))*framework.TargetedWorkerSmokeTimeout)
	defer cancel()
	for _, worker := range s.workers {
		if err := framework.RunTargetedWorkerSmoke(validationCtx, s.runtime, worker); err != nil {
			return err
		}
	}
	return nil
}

func (s *WorkerRollingUpdate) waitForWorkersIdle(ctx context.Context) error {
	timeout := workerRolloutReplaceBase + time.Duration(len(s.initialPods))*workerRolloutReplacePerPod + workerRolloutIdleTimeout
	if s.mk8s.groupID != "" {
		timeout = workerMK8sTimeout
	} else {
		for _, target := range s.targets {
			if target.replacement != nil && target.replacement.maintenanceRequested {
				timeout = nodeReplacementRemoveTimeout + nodeReplacementReadyTimeout
				break
			}
		}
	}
	return s.waitForIdle(ctx, fmt.Sprintf("NodeSet %s workers idle", s.nodeSet.Metadata.Name), timeout)
}

func (s *WorkerRollingUpdate) waitForIdle(ctx context.Context, description string, timeout time.Duration) error {
	names := framework.WorkerNames(s.workers)
	if len(names) == 0 {
		return fmt.Errorf("wait for idle workers: no active workers were selected")
	}
	return s.runtime.WaitFor(ctx, description, timeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			nodes, err := s.slurm.NodeInfosOnce(waitCtx, names)
			if err != nil {
				return false, err
			}
			if problems := idleProblems(nodes); len(problems) > 0 {
				return false, fmt.Errorf("%s", strings.Join(problems, "; "))
			}
			return true, nil
		})
}

func (s *WorkerRollingUpdate) systemDaemonSets(ctx context.Context) ([]appsv1.DaemonSet, error) {
	var daemonSets appsv1.DaemonSetList
	if err := s.kubectl.GetJSON(ctx, &daemonSets, "get", "daemonsets", "-n", framework.SoperatorSystemNamespace, "-o", "json"); err != nil {
		return nil, fmt.Errorf("list DaemonSets in %s: %w", framework.SoperatorSystemNamespace, err)
	}
	return daemonSets.Items, nil
}

// workerRolloutCandidateProblem explains why a NodeSet cannot drive the scenario, or returns "" for a fit.
func workerRolloutCandidateProblem(nodeSet kubeobjects.NodeSet, clusterName string, activeCount int) string {
	if reason := workerRolloutConfigurationProblem(nodeSet, clusterName); reason != "" {
		return reason
	}
	if int(nodeSet.Status.Replicas) != activeCount || nodeSet.Status.Phase != kubeobjects.NodeSetPhaseReady {
		return fmt.Sprintf("phase=%s ready=%d/%d", nodeSet.Status.Phase, nodeSet.Status.Replicas, activeCount)
	}
	return ""
}

func workerRolloutConfigurationProblem(nodeSet kubeobjects.NodeSet, clusterName string) string {
	switch {
	case nodeSet.Spec.ClusterName != "" && nodeSet.Spec.ClusterName != clusterName:
		return "belongs to another cluster"
	case nodeSet.Spec.UpdateStrategy != kubeobjects.NodeSetUpdateStrategySlurmAwareRollingUpdate:
		return fmt.Sprintf("updateStrategy=%q", nodeSet.Spec.UpdateStrategy)
	case nodeSet.Spec.Replicas == 0:
		return "no replicas"
	}
	return ""
}

func daemonSetRolledOut(daemonSet appsv1.DaemonSet, previousGeneration int64) bool {
	return daemonSet.Generation > previousGeneration && daemonSetConverged(daemonSet)
}

func daemonSetConverged(daemonSet appsv1.DaemonSet) bool {
	status := daemonSet.Status
	return daemonSet.DeletionTimestamp == nil && status.ObservedGeneration >= daemonSet.Generation &&
		status.UpdatedNumberScheduled == status.DesiredNumberScheduled &&
		status.NumberAvailable == status.DesiredNumberScheduled &&
		status.NumberUnavailable == 0
}

func idleProblems(nodes []framework.SlurmNodeInfo) []string {
	var problems []string
	for _, node := range nodes {
		if !node.HasStateFlag("IDLE") || !node.IsUsable() {
			problems = append(problems, fmt.Sprintf("%s state=%s", node.Name, node.State))
		}
	}
	return problems
}

func workerRolloutJobGateWait() time.Duration {
	preparation := 2 * workerRolloutJobStartWait
	maintenance := 2*nodeReplacementDrainTimeout + workerRolloutDrainTimeout
	return preparation + max(workerMK8sTimeout, maintenance) + 2*workerRolloutJobFinishWait
}

func workerRolloutBudget(value *intstr.IntOrString, replicas int) (int, error) {
	if replicas < 1 {
		return 0, fmt.Errorf("require positive worker replica count, got %d", replicas)
	}
	if value == nil {
		return 1, nil
	}
	budget, err := intstr.GetScaledValueFromIntOrPercent(value, replicas, false)
	if err != nil {
		return 0, fmt.Errorf("parse maxUnavailable %q: %w", value.String(), err)
	}
	return min(replicas, max(1, budget)), nil
}

func rolloutInvariant(format string, args ...any) error {
	return rolloutInvariantError{fmt.Errorf(format, args...)}
}

// WaitFor retries transient errors; an observed safety violation must survive later recovery.
func (s *WorkerRollingUpdate) waitForRollout(ctx context.Context, description string, timeout, interval time.Duration, condition func(context.Context) (bool, error)) error {
	var fatal error
	err := s.runtime.WaitFor(ctx, description, timeout, interval, func(waitCtx context.Context) (bool, error) {
		err := s.checkRolloutWorkerSet(waitCtx)
		var done bool
		if err == nil {
			done, err = condition(waitCtx)
		}
		var invariant rolloutInvariantError
		if errors.As(err, &invariant) {
			fatal = err
			return true, nil
		}
		return done, err
	})
	if fatal != nil {
		return fatal
	}
	return err
}

func isEphemeralRollout(nodeSet kubeobjects.NodeSet) bool {
	return nodeSet.Spec.EphemeralNodes != nil && *nodeSet.Spec.EphemeralNodes
}

func (s *WorkerRollingUpdate) rolloutWorkerNames(ctx context.Context, nodeSet kubeobjects.NodeSet) ([]string, error) {
	var active []int32
	if isEphemeralRollout(nodeSet) {
		var powerState kubeobjects.NodeSetPowerState
		if err := s.kubectl.GetJSON(ctx, &powerState, "get", "nodesetpowerstate", nodeSet.Metadata.Name,
			"-n", nodeSet.Metadata.Namespace, "-o", "json"); err != nil {
			return nil, fmt.Errorf("read active ordinals for NodeSet %s: %w", nodeSet.Metadata.Name, err)
		}
		active = powerState.Spec.ActiveNodes
	}
	return rolloutWorkerNames(nodeSet, active)
}

func rolloutWorkerNames(nodeSet kubeobjects.NodeSet, active []int32) ([]string, error) {
	if nodeSet.Spec.Replicas < 0 {
		return nil, fmt.Errorf("validate NodeSet %s replicas: %d", nodeSet.Metadata.Name, nodeSet.Spec.Replicas)
	}
	if !isEphemeralRollout(nodeSet) {
		active = make([]int32, nodeSet.Spec.Replicas)
		for ordinal := range active {
			active[ordinal] = int32(ordinal)
		}
	}
	names := make([]string, 0, len(active))
	seen := make(map[int32]bool, len(active))
	for _, ordinal := range active {
		if ordinal < 0 || ordinal >= nodeSet.Spec.Replicas || seen[ordinal] {
			return nil, fmt.Errorf("validate NodeSet %s active ordinals: duplicate or out-of-range ordinal %d for replicas=%d",
				nodeSet.Metadata.Name, ordinal, nodeSet.Spec.Replicas)
		}
		seen[ordinal] = true
		names = append(names, fmt.Sprintf("%s-%d", nodeSet.Metadata.Name, ordinal))
	}
	sort.Strings(names)
	return names, nil
}

// Membership must stay fixed while its snapshot defines replacement checks. Idle power saving is excluded, but external power changes can still
// invalidate the experiment and must not silently reduce its coverage.
func (s *WorkerRollingUpdate) checkRolloutWorkerSet(ctx context.Context) error {
	if !isEphemeralRollout(s.nodeSet) || len(s.workerNames) == 0 {
		return nil
	}
	names, err := s.rolloutWorkerNames(ctx, s.nodeSet)
	if err != nil {
		return err
	}
	if !slices.Equal(names, s.workerNames) {
		return rolloutInvariant("NodeSet %s active workers changed during rollout: expected %v, found %v", s.nodeSet.Metadata.Name, s.workerNames, names)
	}
	return s.checkEphemeralWorkerHold(ctx)
}

func requireRolloutPodNames(names []string, pods []corev1.Pod) error {
	actual := make([]string, 0, len(pods))
	for _, pod := range pods {
		actual = append(actual, pod.Name)
	}
	sort.Strings(actual)
	if !slices.Equal(names, actual) {
		return fmt.Errorf("match active worker pods: expected %v, found %v", names, actual)
	}
	return nil
}

func selectRolloutWorkers(names []string, pool []framework.WorkerInfo) ([]framework.WorkerInfo, error) {
	byName := make(map[string]framework.WorkerInfo, len(pool))
	for _, worker := range pool {
		if _, duplicate := byName[worker.Name]; duplicate {
			return nil, fmt.Errorf("find unique Slurm worker %s", worker.Name)
		}
		byName[worker.Name] = worker
	}
	workers := make([]framework.WorkerInfo, 0, len(names))
	for _, name := range names {
		worker, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("find active worker %s in Slurm main partition", name)
		}
		workers = append(workers, worker)
	}
	return workers, nil
}

func (s *WorkerRollingUpdate) holdEphemeralWorkers(ctx context.Context, names []string) error {
	if len(s.power.names) != 0 {
		return fmt.Errorf("establish ephemeral worker suspension protection: already established")
	}
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	if len(names) == 0 {
		return fmt.Errorf("protect ephemeral workers: empty worker set")
	}
	for _, name := range names {
		if !framework.IsSlurmNodeName(name) {
			return fmt.Errorf("invalid ephemeral worker name %q", name)
		}
	}
	current, err := s.readWorkerSuspendExclusions(ctx)
	if err != nil {
		return err
	}
	var added []string
	for _, name := range names {
		if !current.Names[name] {
			added = append(added, name)
		}
	}
	if len(added) != 0 && current.Counted {
		return fmt.Errorf("protect ephemeral workers %s: SuspendExcNodes uses counted exclusions (:count), which do not support incremental updates; explicitly exclude these workers before running the scenario", strings.Join(added, ","))
	}
	// Record ownership before the write: a failed response may still have applied it.
	s.power = workerRolloutPower{names: names, added: added}
	if len(added) != 0 {
		if err := s.slurm.AddSuspendExclusions(ctx, added); err != nil {
			return fmt.Errorf("exclude ephemeral workers %s from automatic suspension: %w", strings.Join(added, ","), err)
		}
	}
	return s.checkEphemeralWorkerHold(ctx)
}

func (s *WorkerRollingUpdate) checkEphemeralWorkerHold(ctx context.Context) error {
	if len(s.power.names) == 0 {
		return nil
	}
	current, err := s.readWorkerSuspendExclusions(ctx)
	if err != nil {
		return err
	}
	var missing []string
	for _, name := range s.power.names {
		if !current.Names[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		return rolloutInvariant("ephemeral workers %s lost automatic suspension protection in SuspendExcNodes", strings.Join(missing, ","))
	}
	return nil
}

func (s *WorkerRollingUpdate) cleanupEphemeralWorkerHold(ctx context.Context) {
	defer func() { s.power = workerRolloutPower{} }()
	if len(s.power.added) == 0 {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workerRolloutCleanupTimeout)
	defer cancel()
	if err := s.releaseEphemeralWorkers(cleanupCtx); err != nil {
		s.runtime.Logf("Cleanup: restore automatic suspension for ephemeral workers %s: %v; check SuspendExcNodes and remove only these scenario additions", strings.Join(s.power.added, ","), err)
	}
}

func (s *WorkerRollingUpdate) releaseEphemeralWorkers(ctx context.Context) error {
	current, err := s.readWorkerSuspendExclusions(ctx)
	if err != nil {
		return err
	}
	var remove []string
	for _, name := range s.power.added {
		if current.Names[name] {
			remove = append(remove, name)
		}
	}
	if len(remove) == 0 {
		return nil
	}
	if current.Counted {
		return fmt.Errorf("SuspendExcNodes now uses counted exclusions (:count); refusing an incremental removal that Slurm does not support")
	}
	if err := s.slurm.RemoveSuspendExclusions(ctx, remove); err != nil {
		return fmt.Errorf("remove temporary SuspendExcNodes entries: %w", err)
	}
	after, err := s.readWorkerSuspendExclusions(ctx)
	if err != nil {
		return err
	}
	for _, name := range remove {
		if after.Names[name] {
			return fmt.Errorf("temporary SuspendExcNodes entry %s remains after removal", name)
		}
	}
	return nil
}

func (s *WorkerRollingUpdate) readWorkerSuspendExclusions(ctx context.Context) (framework.SlurmSuspendExclusions, error) {
	exclusions, err := s.slurm.SuspendExclusions(ctx)
	var configurationError framework.SlurmConfigurationError
	if errors.As(err, &configurationError) {
		return exclusions, rolloutInvariant("read Slurm automatic suspension exclusions: %v", err)
	}
	return exclusions, err
}
