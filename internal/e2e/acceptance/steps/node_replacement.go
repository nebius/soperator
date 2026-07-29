package steps

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	nodeReplacementFeatureFile = "node_replacement.feature"

	// These timeouts intentionally leave slack for slower replacement flows; tune
	// them together with the CI step budget when this scenario evolves.
	nodeReplacementJobTimeout    = 5 * time.Minute
	nodeReplacementDrainTimeout  = 10 * time.Minute
	nodeReplacementRemoveTimeout = 15 * time.Minute
	nodeReplacementReadyTimeout  = 20 * time.Minute
)

type NodeReplacement struct {
	exec               framework.Exec
	slurm              *framework.SlurmClient
	replacementWorker  framework.WorkerRef
	originalInstanceID string
	maintenanceJob     framework.SbatchJob
	preExistingWorkers []string
	gpuWorkers         map[string]struct{}
}

func NewNodeReplacement(exec framework.Exec, slurm *framework.SlurmClient) *NodeReplacement {
	return &NodeReplacement{
		exec:  exec,
		slurm: slurm,
	}
}

func (s *NodeReplacement) Register(sc *godog.ScenarioContext) {
	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		if path.Base(scenario.Uri) != nodeReplacementFeatureFile || s.maintenanceJob.IsZero() {
			return ctx, nil
		}
		if cancelErr := s.cancelJob(context.Background(), s.maintenanceJob.ID); cancelErr != nil {
			s.exec.Logf("cleanup: cancel maintenance job: %v", cancelErr)
		}
		return ctx, nil
	})

	sc.Step(`^a test job is submitted and running on a (CPU|GPU) worker node$`, s.aTestJobIsSubmittedAndRunningOnWorkerNode)
	sc.Step(`^a maintenance event is triggered for that node$`, s.aMaintenanceEventIsTriggeredForThatNode)
	sc.Step(`^the node is drained with a maintenance reason$`, s.theNodeIsDrainedWithAMaintenanceReason)
	sc.Step(`^the test job is cancelled$`, s.theTestJobIsCancelled)
	sc.Step(`^the old instance is removed$`, s.theOldInstanceIsRemoved)
	sc.Step(`^a replacement node joins the cluster$`, s.aReplacementNodeJoinsTheCluster)
	sc.Step(`^the replacement node passes GPU validation$`, s.theReplacementNodePassesGPUValidation)
	sc.Step(`^all pre-existing worker nodes are operational$`, s.allPreExistingWorkerNodesAreOperational)
}

func (s *NodeReplacement) aTestJobIsSubmittedAndRunningOnWorkerNode(ctx context.Context, workerType string) error {
	workers, err := s.anyWorkersByType(workerType, 1)
	if err != nil {
		return err
	}
	return s.submitTestJobOnWorker(ctx, workers[0])
}

func (s *NodeReplacement) anyWorkersByType(workerType string, count int) ([]framework.WorkerRef, error) {
	switch strings.ToUpper(workerType) {
	case "CPU":
		return s.slurm.AnyCPUWorkers(count)
	case "GPU":
		return s.slurm.AnyGPUWorkers(count)
	default:
		return nil, fmt.Errorf("unsupported worker type %q", workerType)
	}
}

func (s *NodeReplacement) submitTestJobOnWorker(ctx context.Context, worker framework.WorkerRef) error {
	s.replacementWorker = worker
	s.preExistingWorkers = workerNamesFromRefs(s.exec.AvailableWorkers(framework.WorkerAny))
	s.gpuWorkers = workerNameSet(s.exec.AvailableWorkers(framework.WorkerGPU))

	node, err := s.slurm.NodeInfo(ctx, s.replacementWorker.Name)
	if err != nil {
		return fmt.Errorf("read original node state: %w", err)
	}

	if node.InstanceID == "" {
		return fmt.Errorf("parse InstanceId for %s: no match in state=%s reason=%s", s.replacementWorker.Name, node.State, node.Reason)
	}
	s.originalInstanceID = node.InstanceID

	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:    "e2e-node-replacement",
		ExtraFlags: []string{fmt.Sprintf("-w %s", framework.ShellQuote(s.replacementWorker.Name))},
		Wrap:       "sleep 600",
	})
	if err != nil {
		return err
	}
	s.maintenanceJob = job
	s.exec.Logf("node replacement: submitted maintenance job id=%s stdout=%s stderr=%s",
		job.ID, job.StdoutPath, job.StderrPath)

	return s.slurm.WaitForJobRunning(ctx, job.ID, nodeReplacementJobTimeout)
}

func (s *NodeReplacement) aMaintenanceEventIsTriggeredForThatNode(ctx context.Context) error {
	patch := fmt.Sprintf(
		`{"status":{"conditions":[{"type":"NebiusMaintenanceScheduled","status":"True","reason":"AcceptanceTest","message":"Maintenance scheduled for node","lastTransitionTime":"%s"}]}}`,
		time.Now().UTC().Format(time.RFC3339))
	if _, err := s.exec.Kubectl().Run(ctx, "patch", "node", s.originalInstanceID,
		"--subresource=status", "--type=strategic", "-p", patch); err != nil {
		return fmt.Errorf("patch maintenance condition: %w", err)
	}
	return nil
}

func (s *NodeReplacement) theNodeIsDrainedWithAMaintenanceReason(ctx context.Context) error {
	workerName := s.replacementWorker.Name
	return s.exec.WaitFor(ctx, "node drain reason", nodeReplacementDrainTimeout, 15*time.Second, func(waitCtx context.Context) (bool, error) {
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
	originalInstanceID := s.originalInstanceID
	return s.exec.WaitFor(ctx, "old instance removal", nodeReplacementRemoveTimeout, 30*time.Second, func(waitCtx context.Context) (bool, error) {
		_, err := s.exec.Local().Run(waitCtx, "nebius", "compute", "instance", "get", "--id", originalInstanceID, "--format", "json")
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

func (s *NodeReplacement) aReplacementNodeJoinsTheCluster(ctx context.Context) error {
	workerName := s.replacementWorker.Name
	originalInstanceID := s.originalInstanceID
	return s.exec.WaitFor(ctx, "replacement node ready", nodeReplacementReadyTimeout, 60*time.Second, func(waitCtx context.Context) (bool, error) {
		node, err := s.slurm.NodeInfo(waitCtx, workerName)
		if err != nil {
			return false, err
		}

		if node.InstanceID == "" || node.InstanceID == originalInstanceID {
			return false, nil
		}

		if node.State == "" {
			return false, nil
		}
		if !node.IsUsable() {
			return false, nil
		}

		return true, nil
	})
}

func (s *NodeReplacement) theReplacementNodePassesGPUValidation(ctx context.Context) error {
	workerName := s.replacementWorker.Name
	if _, err := s.exec.Jail().Run(ctx, fmt.Sprintf("srun -w %s --gpus-per-node=1 nvidia-smi -L >/dev/null", framework.ShellQuote(workerName))); err != nil {
		node, stateErr := s.slurm.NodeInfo(ctx, workerName)
		if stateErr == nil {
			s.exec.Logf("replacement worker state after failed final validation: name=%s state=%s reason=%s instance_id=%s",
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

	for _, workerName := range s.preExistingWorkers {
		if err := s.validateWorkerNodeState(ctx, workerName); err != nil {
			return err
		}
		if _, isGPU := s.gpuWorkers[workerName]; isGPU {
			if err := s.validateGPUWorker(ctx, workerName); err != nil {
				return fmt.Errorf("validate pre-existing GPU worker %s: %w", workerName, err)
			}
			continue
		}
		if err := s.validateCPUWorker(ctx, workerName); err != nil {
			return fmt.Errorf("validate pre-existing CPU worker %s: %w", workerName, err)
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

func (s *NodeReplacement) validateCPUWorker(ctx context.Context, workerName string) error {
	if _, err := s.exec.Jail().Run(ctx, fmt.Sprintf("srun -w %s true", framework.ShellQuote(workerName))); err != nil {
		return fmt.Errorf("validate worker accepts a targeted Slurm job: %w", err)
	}
	return nil
}

func (s *NodeReplacement) validateGPUWorker(ctx context.Context, workerName string) error {
	if _, err := s.exec.Jail().Run(ctx, fmt.Sprintf("srun -w %s --gpus-per-node=1 nvidia-smi -L >/dev/null", framework.ShellQuote(workerName))); err != nil {
		return fmt.Errorf("validate worker GPU from login node: %w", err)
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

func workerNamesFromRefs(workers []framework.WorkerRef) []string {
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		if strings.TrimSpace(worker.Name) == "" {
			continue
		}
		names = append(names, worker.Name)
	}
	return names
}

func workerNameSet(workers []framework.WorkerRef) map[string]struct{} {
	names := make(map[string]struct{}, len(workers))
	for _, worker := range workers {
		if strings.TrimSpace(worker.Name) == "" {
			continue
		}
		names[worker.Name] = struct{}{}
	}
	return names
}
