package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	// These timeouts intentionally leave slack for slower replacement flows; tune
	// them together with the CI step budget when this scenario evolves.
	nodeReplacementJobTimeout    = 5 * time.Minute
	nodeReplacementDrainTimeout  = 10 * time.Minute
	nodeReplacementRemoveTimeout = 15 * time.Minute
	nodeReplacementReadyTimeout  = 20 * time.Minute
)

type NodeReplacement struct {
	runtime            framework.Runtime
	slurm              *framework.SlurmClient
	selector           *framework.WorkerSelector
	replacementWorker  framework.WorkerInfo
	originalInstanceID string
	maintenanceJob     framework.SbatchJob
	preExistingWorkers []framework.WorkerInfo
}

func NewNodeReplacement(runtime framework.Runtime, slurm *framework.SlurmClient, selector *framework.WorkerSelector) *NodeReplacement {
	return &NodeReplacement{
		runtime:  runtime,
		slurm:    slurm,
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
	if !s.maintenanceJob.IsZero() {
		if cancelErr := s.cancelJob(ctx, s.maintenanceJob.ID); cancelErr != nil {
			s.runtime.Logf("cleanup: cancel maintenance job: %v", cancelErr)
		}
	}
	s.replacementWorker = framework.WorkerInfo{}
	s.originalInstanceID = ""
	s.maintenanceJob = framework.SbatchJob{}
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
	s.replacementWorker = worker
	s.preExistingWorkers = append([]framework.WorkerInfo(nil), snapshot.Workers...)

	node, err := s.slurm.NodeInfo(ctx, s.replacementWorker.Name)
	if err != nil {
		return fmt.Errorf("read original node state: %w", err)
	}

	if node.InstanceID == "" {
		return fmt.Errorf("parse InstanceId for %s: no match in state=%s reason=%s", s.replacementWorker.Name, node.State, node.Reason)
	}
	s.originalInstanceID = node.InstanceID

	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:  "e2e-node-replacement",
		Nodelist: []string{s.replacementWorker.Name},
		Wrap:     "sleep 600",
	})
	if err != nil {
		return err
	}
	s.maintenanceJob = job
	s.runtime.Logf("node replacement: submitted maintenance job id=%s stdout=%s stderr=%s",
		job.ID, job.StdoutPath, job.StderrPath)

	return s.slurm.WaitForJobRunning(ctx, job.ID, nodeReplacementJobTimeout)
}

func (s *NodeReplacement) aMaintenanceEventIsTriggeredForThatNode(ctx context.Context) error {
	patch := fmt.Sprintf(
		`{"status":{"conditions":[{"type":"NebiusMaintenanceScheduled","status":"True","reason":"AcceptanceTest","message":"Maintenance scheduled for node","lastTransitionTime":"%s"}]}}`,
		time.Now().UTC().Format(time.RFC3339))
	if _, err := s.runtime.Kubectl().Run(ctx, "patch", "node", s.originalInstanceID,
		"--subresource=status", "--type=strategic", "-p", patch); err != nil {
		return fmt.Errorf("patch maintenance condition: %w", err)
	}
	return nil
}

func (s *NodeReplacement) theNodeIsDrainedWithAMaintenanceReason(ctx context.Context) error {
	workerName := s.replacementWorker.Name
	return s.runtime.WaitFor(ctx, "node drain reason", nodeReplacementDrainTimeout, 15*time.Second, func(waitCtx context.Context) (bool, error) {
		node, err := s.slurm.NodeInfo(waitCtx, workerName)
		if err != nil {
			return false, err
		}
		return node.HasStateFlag("DRAIN") && strings.HasPrefix(node.Reason, "[compute_maintenance]"), nil
	})
}

func (s *NodeReplacement) theTestJobIsCancelled(ctx context.Context) error {
	if err := s.cancelJob(ctx, s.maintenanceJob.ID); err != nil {
		return err
	}
	s.maintenanceJob = framework.SbatchJob{}
	return nil
}

func (s *NodeReplacement) theOldInstanceIsRemoved(ctx context.Context) error {
	return framework.WaitForInstanceRemoval(ctx, s.runtime, s.originalInstanceID, nodeReplacementRemoveTimeout, 30*time.Second)
}

func (s *NodeReplacement) aReplacementNodeJoinsTheCluster(ctx context.Context) error {
	return framework.WaitForUsableReplacement(ctx, s.runtime, s.slurm, s.replacementWorker.Name, s.originalInstanceID, nodeReplacementReadyTimeout, 60*time.Second)
}

func (s *NodeReplacement) theReplacementNodePassesGPUValidation(ctx context.Context) error {
	if err := runTargetedWorkerSmoke(ctx, s.runtime, s.replacementWorker); err != nil {
		node, stateErr := s.slurm.NodeInfo(ctx, s.replacementWorker.Name)
		if stateErr == nil {
			s.runtime.Logf("replacement worker state after failed final validation: name=%s state=%s reason=%s instance_id=%s",
				node.Name, node.State, node.Reason, node.InstanceID)
		}
		return fmt.Errorf("validate replacement worker is operational from login node: %w", err)
	}
	return nil
}

func (s *NodeReplacement) allPreExistingWorkerNodesAreOperational(ctx context.Context) error {
	if len(s.preExistingWorkers) == 0 {
		return fmt.Errorf("no pre-existing workers were recorded before replacement")
	}

	for _, worker := range s.preExistingWorkers {
		if err := s.validateWorkerNodeState(ctx, worker.Name); err != nil {
			return err
		}
		if err := runTargetedWorkerSmoke(ctx, s.runtime, worker); err != nil {
			return fmt.Errorf("validate pre-existing worker %s: %w", worker.Name, err)
		}
	}
	return nil
}

func (s *NodeReplacement) validateWorkerNodeState(ctx context.Context, workerName string) error {
	node, err := s.slurm.NodeInfo(ctx, workerName)
	if err != nil {
		return fmt.Errorf("read worker node state %s: %w", workerName, err)
	}
	if node.State == "" {
		return fmt.Errorf("parse worker node state %s: no State field", workerName)
	}
	if !node.IsUsable() {
		return fmt.Errorf("worker node %s has bad state=%s reason=%s", workerName, node.State, node.Reason)
	}
	return nil
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
