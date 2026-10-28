package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	nodeReplacementMaintenanceCondition = "NebiusMaintenanceScheduled"
	nodeReplacementDrainReason          = "[compute_maintenance] node replacement process"

	// soperatorchecks replaces the node for any check that drains with a reason with this prefix.
	nodeReplacementHealthCheckReason = "[node_problem]"
	// healthCheckInjectErrorReasonPart is the health-checker check name reported for --inject-error,
	// see docs/passive-checks.md. It separates the simulated failure from a real [node_problem] drain.
	healthCheckInjectErrorReasonPart = "inject_error"

	// gpu_health_check passes --inject-error to the health-checker while this node-local marker exists,
	// so only the Soperator run of the health-checker fails and nothing reaches platform monitoring of the host.
	healthCheckInjectErrorMarker     = "/opt/soperator-outputs/local/health_checker_inject_error"
	healthCheckInjectErrorResolution = "RedeployProfile"

	// These timeouts intentionally leave slack for slower replacement flows; tune
	// them together with the CI step budget when this scenario evolves. Compute
	// has been observed to drop a deleted Kubernetes node's instance more than
	// 20 minutes after the replacement node was already serving the worker.
	nodeReplacementJobTimeout    = 5 * time.Minute
	nodeReplacementDrainTimeout  = 10 * time.Minute
	nodeReplacementRemoveTimeout = 30 * time.Minute
	nodeReplacementReadyTimeout  = 20 * time.Minute
	nodeReplacementJobHold       = 30 * time.Minute

	// Slurm runs HealthCheckProgram on its own schedule: with HealthCheckNodeState=CYCLE
	// the next run on a node starts within one HealthCheckInterval, and one more
	// interval covers the check itself and the reason reaching slurmctld.
	nodeReplacementHealthCheckDrainMinWait = 2 * time.Minute
	nodeReplacementHealthCheckDrainMaxWait = 10 * time.Minute
)

type NodeReplacement struct {
	runtime              framework.Runtime
	slurm                *framework.SlurmClient
	kubectl              *framework.KubectlClient
	selector             *framework.WorkerSelector
	replacementWorker    framework.WorkerInfo
	original             framework.WorkerReplacementSnapshot
	maintenanceJob       framework.SbatchJob
	maintenanceRequested bool
	faultInjected        bool
	preExistingWorkers   []framework.WorkerInfo
}

func NewNodeReplacement(runtime framework.Runtime, slurm *framework.SlurmClient, selector *framework.WorkerSelector) *NodeReplacement {
	return &NodeReplacement{
		runtime:  runtime,
		slurm:    slurm,
		kubectl:  framework.NewKubectlClient(runtime),
		selector: selector,
	}
}

func (s *NodeReplacement) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a test job is submitted and running on a (CPU|GPU) worker node$`, s.aTestJobIsSubmittedAndRunningOnWorkerNode)
	sc.Step(`^a test job is submitted and running on an idle GPU worker node$`, s.aTestJobIsSubmittedAndRunningOnAnIdleGPUWorkerNode)
	sc.Step(`^a maintenance event is triggered for that node$`, s.aMaintenanceEventIsTriggeredForThatNode)
	sc.Step(`^the node is drained with a maintenance reason$`, s.theNodeIsDrainedWithAMaintenanceReason)
	sc.Step(`^a GPU health check failure is simulated on that node$`, s.aGPUHealthCheckFailureIsSimulatedOnThatNode)
	sc.Step(`^the node is drained with a node problem reason$`, s.theNodeIsDrainedWithANodeProblemReason)
	sc.Step(`^the node is draining with a node problem reason while the test job is running$`, s.theNodeIsDrainingWithANodeProblemReasonWhileTheTestJobIsRunning)
	sc.Step(`^a maintenance event is triggered while that node is draining$`, s.aMaintenanceEventIsTriggeredWhileThatNodeIsDraining)
	sc.Step(`^the node is draining with a maintenance reason while the test job is running$`, s.theNodeIsDrainingWithAMaintenanceReasonWhileTheTestJobIsRunning)
	sc.Step(`^a maintenance event is triggered after that node has fully drained$`, s.aMaintenanceEventIsTriggeredAfterThatNodeHasFullyDrained)
	sc.Step(`^the test job is cancelled$`, s.theTestJobIsCancelled)
	sc.Step(`^the old instance is removed$`, s.theOldInstanceIsRemoved)
	sc.Step(`^a replacement node joins the cluster$`, s.aReplacementNodeJoinsTheCluster)
	sc.Step(`^the replacement node passes GPU validation$`, s.theReplacementNodePassesGPUValidation)
	sc.Step(`^all pre-existing worker nodes are operational$`, s.allPreExistingWorkerNodesAreOperational)
}

func (s *NodeReplacement) CleanupAndReset(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), nodeReplacementRemoveTimeout+nodeReplacementReadyTimeout+time.Minute)
	defer cancel()
	if !s.maintenanceJob.IsZero() {
		if cancelErr := s.cancelJob(cleanupCtx, s.maintenanceJob.ID); cancelErr != nil {
			s.runtime.Logf("Cleanup: cancel maintenance job: %v", cancelErr)
		}
	}
	if s.maintenanceRequested {
		if err := s.finishMaintenance(cleanupCtx); err != nil {
			s.runtime.Logf("Cleanup: finish maintenance for worker %s, original host %s/%s, instance %s: %v",
				s.original.WorkerName, s.original.NodeName, s.original.NodeUID, s.original.InstanceID, err)
		}
	}
	if s.faultInjected {
		s.removeHealthCheckInjectErrorMarker(cleanupCtx)
		if err := s.finishHealthCheckReplacement(cleanupCtx); err != nil {
			s.runtime.Logf("Cleanup: finish health check replacement for worker %s, original host %s/%s, instance %s: %v",
				s.original.WorkerName, s.original.NodeName, s.original.NodeUID, s.original.InstanceID, err)
		}
	}
	s.replacementWorker = framework.WorkerInfo{}
	s.original = framework.WorkerReplacementSnapshot{}
	s.maintenanceJob = framework.SbatchJob{}
	s.maintenanceRequested = false
	s.faultInjected = false
	s.preExistingWorkers = nil
}

func (s *NodeReplacement) aTestJobIsSubmittedAndRunningOnWorkerNode(ctx context.Context, workerType string) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	workers, err := snapshot.PickWorkersByType(workerType, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	return s.submitTestJobOnWorker(ctx, workers[0], snapshot)
}

func (s *NodeReplacement) aTestJobIsSubmittedAndRunningOnAnIdleGPUWorkerNode(ctx context.Context) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	workers, err := pickIdleGPUWorkers(snapshot, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	return s.submitTestJobOnWorker(ctx, workers[0], snapshot)
}

func (s *NodeReplacement) submitTestJobOnWorker(ctx context.Context, worker framework.WorkerInfo, snapshot framework.WorkerSnapshot) error {
	if err := s.selectWorker(ctx, worker); err != nil {
		return err
	}
	s.preExistingWorkers = append([]framework.WorkerInfo(nil), snapshot.Workers...)

	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:  "e2e-node-replacement",
		Nodelist: []string{s.replacementWorker.Name},
		Wrap:     fmt.Sprintf("sleep %.0f", nodeReplacementJobHold.Seconds()),
	})
	if err != nil {
		return err
	}
	s.maintenanceJob = job
	s.runtime.Logf("Node replacement: submitted maintenance job id=%s stdout=%s stderr=%s",
		job.ID, job.StdoutPath, job.StderrPath)

	return s.slurm.WaitForJobRunning(ctx, job.ID, nodeReplacementJobTimeout)
}

func (s *NodeReplacement) selectWorker(ctx context.Context, worker framework.WorkerInfo) error {
	original, err := framework.CaptureWorkerReplacement(ctx, s.kubectl, s.slurm, worker.Name)
	if err != nil {
		return fmt.Errorf("record original worker for replacement: %w", err)
	}
	s.replacementWorker = worker
	s.original = original
	return nil
}

func (s *NodeReplacement) aMaintenanceEventIsTriggeredForThatNode(ctx context.Context) error {
	// Keep ownership even if the patch succeeds but its response is lost.
	s.maintenanceRequested = true
	return triggerNodeMaintenance(ctx, s.runtime, s.original.NodeName)
}

func triggerNodeMaintenance(ctx context.Context, runtime framework.Runtime, nodeName string) error {
	condition := corev1.NodeCondition{
		Type:               corev1.NodeConditionType(nodeReplacementMaintenanceCondition),
		Status:             corev1.ConditionTrue,
		Reason:             "AcceptanceTest",
		Message:            "Maintenance scheduled for node",
		LastTransitionTime: metav1.NewTime(time.Now().UTC()),
	}
	patch, err := json.Marshal(map[string]any{"status": map[string]any{"conditions": []corev1.NodeCondition{condition}}})
	if err != nil {
		return fmt.Errorf("encode maintenance condition: %w", err)
	}
	if _, err := runtime.Kubectl().Run(ctx, "patch", "node", nodeName,
		"--subresource=status", "--type=strategic", "-p", string(patch)); err != nil {
		return fmt.Errorf("patch maintenance condition: %w", err)
	}
	return nil
}

func maintenanceReasonMatches(reason string) bool {
	return strings.HasPrefix(strings.TrimSpace(reason), "[compute_maintenance]")
}

func (s *NodeReplacement) theNodeIsDrainedWithAMaintenanceReason(ctx context.Context) error {
	return s.waitForMaintenanceDrain(ctx, nil)
}

func (s *NodeReplacement) waitForMaintenanceDrain(ctx context.Context, guard func(context.Context) error) error {
	workerName := s.replacementWorker.Name
	return s.waitForReplacementCondition(ctx, "node drain reason", nodeReplacementDrainTimeout, 15*time.Second, func(waitCtx context.Context) (bool, error) {
		if guard != nil {
			if err := guard(waitCtx); err != nil {
				return false, err
			}
		}
		node, err := s.slurm.NodeInfo(waitCtx, workerName)
		if err != nil {
			return false, err
		}
		if !node.HasStateFlag("DRAIN") || !maintenanceReasonMatches(node.Reason) {
			return false, fmt.Errorf("worker %s maintenance drain pending: state=%s reason=%q instance=%s",
				workerName, node.State, node.Reason, node.InstanceID)
		}
		return true, nil
	})
}

func (s *NodeReplacement) theTestJobIsCancelled(ctx context.Context) error {
	if err := s.cancelJob(ctx, s.maintenanceJob.ID); err != nil {
		return err
	}
	return nil
}

func (s *NodeReplacement) theOldInstanceIsRemoved(ctx context.Context) error {
	return framework.WaitForInstanceRemoval(ctx, s.runtime, s.original.InstanceID, nodeReplacementRemoveTimeout, 30*time.Second)
}

func (s *NodeReplacement) aReplacementNodeJoinsTheCluster(ctx context.Context) error {
	return s.waitForReplacement(ctx, nodeReplacementReadyTimeout, nil)
}

func (s *NodeReplacement) waitForReplacement(ctx context.Context, timeout time.Duration, guard func(context.Context) error) error {
	return s.waitForReplacementCondition(ctx, "replacement worker ready on a new host", timeout, 60*time.Second, func(waitCtx context.Context) (bool, error) {
		if guard != nil {
			if err := guard(waitCtx); err != nil {
				return false, err
			}
		}
		pod, ready, err := framework.ObserveWorkerReplacement(waitCtx, s.runtime, s.kubectl, s.slurm, s.original)
		if err != nil || ready {
			return ready, err
		}
		return false, fmt.Errorf("worker %s replacement pending: original host=%s UID=%s instance=%s pod UID=%s host=%s",
			s.original.WorkerName, s.original.NodeName, s.original.NodeUID, s.original.InstanceID, pod.UID, pod.Spec.NodeName)
	})
}

// Runtime.WaitFor retries observation errors; an observed invariant violation must stay fatal.
func (s *NodeReplacement) waitForReplacementCondition(ctx context.Context, description string, timeout, interval time.Duration, condition func(context.Context) (bool, error)) error {
	var fatal error
	err := s.runtime.WaitFor(ctx, description, timeout, interval, func(waitCtx context.Context) (bool, error) {
		if fatal != nil {
			return true, nil
		}
		done, err := condition(waitCtx)
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

// An owned maintenance event must finish; clearing it midway would leave a
// drained or cordoned host behind. A rejected patch owns no condition to finish.
func (s *NodeReplacement) finishMaintenance(ctx context.Context) error {
	host, present, err := s.kubectl.NodeIfPresent(ctx, s.original.NodeName)
	if err != nil {
		return fmt.Errorf("inspect original maintenance host: %w", err)
	}
	if present && host.UID == s.original.NodeUID && !nodeMaintenanceEventOwned(host) {
		return nil
	}
	return s.waitForReplacement(ctx, nodeReplacementRemoveTimeout+nodeReplacementReadyTimeout, nil)
}

func nodeMaintenanceEventOwned(host corev1.Node) bool {
	for _, condition := range host.Status.Conditions {
		if condition.Type == corev1.NodeConditionType(nodeReplacementMaintenanceCondition) &&
			condition.Status == corev1.ConditionTrue && condition.Reason == "AcceptanceTest" {
			return true
		}
	}
	return false
}

func (s *NodeReplacement) aGPUHealthCheckFailureIsSimulatedOnThatNode(ctx context.Context) error {
	if s.replacementWorker.Name == "" {
		return fmt.Errorf("worker is not selected")
	}
	// Mark before running: cleanup must inspect the node even when the command ran
	// but its result never came back.
	s.faultInjected = true
	if _, err := s.runtime.Worker(s.replacementWorker).RunWithDefaultRetry(ctx, healthCheckInjectErrorCommand()); err != nil {
		return fmt.Errorf("simulate GPU health check failure on %s: %w", s.replacementWorker.Name, err)
	}
	s.runtime.Logf("Node replacement: wrote %s with %s on %s", healthCheckInjectErrorMarker, healthCheckInjectErrorResolution, s.replacementWorker.Name)
	return nil
}

func healthCheckInjectErrorCommand() string {
	return fmt.Sprintf("mkdir -p %s && printf '%%s\\n' %s > %s",
		framework.ShellQuote(path.Dir(healthCheckInjectErrorMarker)),
		framework.ShellQuote(healthCheckInjectErrorResolution),
		framework.ShellQuote(healthCheckInjectErrorMarker))
}

// The marker lives on the original node's disk, so it disappears with the node.
// Removing it matters when the node was never replaced: a leftover marker fails
// every later GPU prolog on that node, so the removal is retried before giving up.
func (s *NodeReplacement) removeHealthCheckInjectErrorMarker(ctx context.Context) {
	if s.replacementWorker.Name == "" {
		return
	}
	command := fmt.Sprintf("rm -f %s", framework.ShellQuote(healthCheckInjectErrorMarker))
	if _, err := s.runtime.Worker(s.replacementWorker).RunWithDefaultRetry(ctx, command); err != nil {
		s.runtime.Logf("Cleanup: remove %s from %s: %v; if the node was not replaced, delete the marker by hand or GPU jobs on it will keep failing the prolog check",
			healthCheckInjectErrorMarker, s.replacementWorker.Name, err)
	}
}

func (s *NodeReplacement) theNodeIsDrainedWithANodeProblemReason(ctx context.Context) error {
	return s.waitForHealthCheckDrain(ctx, nil)
}

func (s *NodeReplacement) waitForHealthCheckDrain(ctx context.Context, guard func(context.Context) error) error {
	workerName := s.replacementWorker.Name
	configuration, err := s.slurm.Configuration(ctx)
	if err != nil {
		return err
	}
	timeout, err := healthCheckDrainTimeout(configuration)
	if err != nil {
		return err
	}
	s.runtime.Logf("Node replacement: waiting up to %s for Slurm's periodic health check to drain %s", timeout, workerName)
	err = s.waitForReplacementCondition(ctx, "simulated health check drain", timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		if guard != nil {
			if err := guard(waitCtx); err != nil {
				return false, err
			}
		}
		node, err := s.slurm.NodeInfo(waitCtx, workerName)
		if err != nil {
			return false, err
		}
		if !node.HasStateFlag("DRAIN") || !healthCheckDrainReasonMatches(node.Reason) {
			return false, fmt.Errorf("worker %s simulated health check drain pending: state=%s reason=%q instance=%s",
				workerName, node.State, node.Reason, node.InstanceID)
		}
		return true, nil
	})
	node, stateErr := s.slurm.NodeInfo(ctx, workerName)
	if stateErr == nil {
		s.runtime.Logf("Worker state after waiting for the health check drain: name=%s state=%s reason=%s instance_id=%s",
			node.Name, node.State, node.Reason, node.InstanceID)
	}
	if err != nil {
		return fmt.Errorf("wait for %s to be drained with a node problem reason from the simulated health check failure: %w", workerName, err)
	}
	return nil
}

// healthCheckDrainReasonMatches accepts a drain caused by the simulated failure only: the
// [node_problem] prefix is what makes soperator replace the node, and the inject_error check
// name proves the marker, not a real failure, drained it. The check name, resolution text and
// the [prolog]/[hc_program] context are left out: they may change or vary with cluster activity.
func healthCheckDrainReasonMatches(reason string) bool {
	return strings.HasPrefix(reason, nodeReplacementHealthCheckReason) &&
		strings.Contains(reason, healthCheckInjectErrorReasonPart)
}

// healthCheckDrainTimeout derives the drain wait from the effective HealthCheckInterval.
// A disabled or very long interval is reported instead of waited out.
func healthCheckDrainTimeout(configuration map[string]string) (time.Duration, error) {
	raw, found := configuration["HealthCheckInterval"]
	if !found {
		return 0, fmt.Errorf("HealthCheckInterval is missing from the effective Slurm configuration")
	}
	interval, err := parseSlurmSeconds(raw)
	if err != nil {
		return 0, fmt.Errorf("parse HealthCheckInterval %q: %w", raw, err)
	}
	if interval == 0 {
		return 0, fmt.Errorf("HealthCheckProgram is disabled: HealthCheckInterval is 0")
	}
	timeout := max(2*interval, nodeReplacementHealthCheckDrainMinWait)
	if timeout > nodeReplacementHealthCheckDrainMaxWait {
		return 0, fmt.Errorf("HealthCheckInterval %s is too long for this scenario: waiting %s exceeds the %s limit",
			interval, timeout, nodeReplacementHealthCheckDrainMaxWait)
	}
	return timeout, nil
}

// parseSlurmSeconds reads scontrol's "<N> sec" duration format.
func parseSlurmSeconds(raw string) (time.Duration, error) {
	value, unit, found := strings.Cut(strings.TrimSpace(raw), " ")
	if !found || strings.TrimSpace(unit) != "sec" {
		return 0, fmt.Errorf("expected \"<seconds> sec\"")
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return 0, fmt.Errorf("expected a non-negative integer number of seconds")
	}
	return time.Duration(seconds) * time.Second, nil
}

// A simulated failure must end in a replacement unless it never took effect.
// Only a drained worker on the original host proves the product noticed it.
func (s *NodeReplacement) finishHealthCheckReplacement(ctx context.Context) error {
	host, present, err := s.kubectl.NodeIfPresent(ctx, s.original.NodeName)
	if err != nil {
		return fmt.Errorf("inspect original health check host: %w", err)
	}
	if present && host.UID == s.original.NodeUID {
		node, err := s.slurm.NodeInfoOnce(ctx, s.original.WorkerName)
		if err != nil {
			return fmt.Errorf("inspect worker after simulated health check failure: %w", err)
		}
		if !node.HasStateFlag("DRAIN") {
			return nil
		}
	}
	return s.waitForReplacement(ctx, nodeReplacementRemoveTimeout+nodeReplacementReadyTimeout, nil)
}

func (s *NodeReplacement) theReplacementNodePassesGPUValidation(ctx context.Context) error {
	if err := framework.RunTargetedWorkerSmoke(ctx, s.runtime, s.replacementWorker); err != nil {
		node, stateErr := s.slurm.NodeInfo(ctx, s.replacementWorker.Name)
		if stateErr == nil {
			s.runtime.Logf("Replacement worker state after failed final validation: name=%s state=%s reason=%s instance_id=%s",
				node.Name, node.State, node.Reason, node.InstanceID)
		}
		return fmt.Errorf("validate replacement worker is operational from login node: %w", err)
	}
	return nil
}

func (s *NodeReplacement) allPreExistingWorkerNodesAreOperational(ctx context.Context) error {
	return validateWorkersOperational(ctx, s.runtime, s.slurm, s.preExistingWorkers)
}

func (s *NodeReplacement) cancelJob(ctx context.Context, maintenanceJobID string) error {
	if maintenanceJobID == "" {
		return nil
	}

	if err := s.slurm.CancelJob(ctx, maintenanceJobID, 0); err != nil {
		return fmt.Errorf("cancel maintenance job %s: %w", maintenanceJobID, err)
	}
	return nil
}
