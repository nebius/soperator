package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	// These timeouts intentionally leave slack for slower replacement flows; tune
	// them together with the CI step budget when this scenario evolves.
	nodeReplacementJobTimeout    = 5 * time.Minute
	nodeReplacementDrainTimeout  = 10 * time.Minute
	nodeReplacementRemoveTimeout = 15 * time.Minute
	nodeReplacementReadyTimeout  = 20 * time.Minute
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
	sc.Step(`^a maintenance event is triggered for that node$`, s.aMaintenanceEventIsTriggeredForThatNode)
	sc.Step(`^the node is drained with a maintenance reason$`, s.theNodeIsDrainedWithAMaintenanceReason)
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
	s.replacementWorker = framework.WorkerInfo{}
	s.original = framework.WorkerReplacementSnapshot{}
	s.maintenanceJob = framework.SbatchJob{}
	s.maintenanceRequested = false
	s.preExistingWorkers = nil
}

func (s *NodeReplacement) aTestJobIsSubmittedAndRunningOnWorkerNode(ctx context.Context, workerType string) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	workers, err := s.anyWorkersByType(snapshot, workerType, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	return s.submitTestJobOnWorker(ctx, workers[0], snapshot)
}

func (s *NodeReplacement) anyWorkersByType(snapshot framework.WorkerSnapshot, workerType string, count int) ([]framework.WorkerInfo, error) {
	switch strings.ToUpper(workerType) {
	case "CPU":
		return snapshot.PickCPUWorkers(count)
	case "GPU":
		return snapshot.PickGPUWorkers(count)
	default:
		return nil, fmt.Errorf("unsupported worker type %q", workerType)
	}
}

func (s *NodeReplacement) submitTestJobOnWorker(ctx context.Context, worker framework.WorkerInfo, snapshot framework.WorkerSnapshot) error {
	if err := s.selectWorker(ctx, worker); err != nil {
		return err
	}
	s.preExistingWorkers = append([]framework.WorkerInfo(nil), snapshot.Workers...)

	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:  "e2e-node-replacement",
		Nodelist: []string{s.replacementWorker.Name},
		Wrap:     "sleep 600",
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
	var guardErr error
	var stop bool
	err := s.runtime.WaitFor(ctx, "node drain reason", nodeReplacementDrainTimeout, 15*time.Second, func(waitCtx context.Context) (bool, error) {
		if stop {
			return true, nil
		}
		if guard != nil {
			err := guard(waitCtx)
			var invariant rolloutInvariantError
			if errors.As(err, &invariant) {
				guardErr = err
				stop = true
			} else if err != nil {
				return false, err
			}
		}
		if stop {
			return true, nil
		}
		node, err := s.slurm.NodeInfo(waitCtx, workerName)
		if err != nil {
			return false, err
		}
		return node.HasStateFlag("DRAIN") && maintenanceReasonMatches(node.Reason), nil
	})
	if guardErr != nil {
		return guardErr
	}
	return err
}

func (s *NodeReplacement) theTestJobIsCancelled(ctx context.Context) error {
	if err := s.cancelJob(ctx, s.maintenanceJob.ID); err != nil {
		return err
	}
	s.maintenanceJob = framework.SbatchJob{}
	return nil
}

func (s *NodeReplacement) theOldInstanceIsRemoved(ctx context.Context) error {
	return framework.WaitForInstanceRemoval(ctx, s.runtime, s.original.InstanceID, nodeReplacementRemoveTimeout, 30*time.Second)
}

func (s *NodeReplacement) aReplacementNodeJoinsTheCluster(ctx context.Context) error {
	return s.waitForReplacement(ctx, nodeReplacementReadyTimeout, nil)
}

func (s *NodeReplacement) waitForReplacement(ctx context.Context, timeout time.Duration, guard func(context.Context) error) error {
	var guardErr error
	var stop bool
	err := s.runtime.WaitFor(ctx, "replacement worker ready on a new host", timeout, 60*time.Second, func(waitCtx context.Context) (bool, error) {
		if stop {
			return true, nil
		}
		if guard != nil {
			err := guard(waitCtx)
			var invariant rolloutInvariantError
			if errors.As(err, &invariant) {
				guardErr = err
				stop = true
			} else if err != nil {
				return false, err
			}
		}
		if stop {
			return true, nil
		}
		pod, ready, err := framework.ObserveWorkerReplacement(waitCtx, s.runtime, s.kubectl, s.slurm, s.original)
		if err != nil || ready {
			return ready, err
		}
		return false, fmt.Errorf("worker %s replacement pending: original host=%s UID=%s instance=%s pod UID=%s host=%s",
			s.original.WorkerName, s.original.NodeName, s.original.NodeUID, s.original.InstanceID, pod.UID, pod.Spec.NodeName)
	})
	if guardErr != nil {
		return guardErr
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
