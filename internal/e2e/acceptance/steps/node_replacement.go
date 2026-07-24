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
	nodeReplacementJobTimeout    = 25 * time.Minute
	nodeReplacementDrainTimeout  = 25 * time.Minute
	nodeReplacementRemoveTimeout = 25 * time.Minute
	nodeReplacementReadyTimeout  = 25 * time.Minute
)

type NodeReplacement struct {
	exec               framework.Exec
	slurm              *framework.SlurmClient
	replacementWorker  framework.WorkerRef
	originalInstanceID string
	maintenanceJob     framework.SbatchJob
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

	sc.Step(`^a test job is submitted and running on a worker node$`, s.aTestJobIsSubmittedAndRunningOnAWorkerNode)
	sc.Step(`^a maintenance event is triggered for that node$`, s.aMaintenanceEventIsTriggeredForThatNode)
	sc.Step(`^the node is drained with a maintenance reason$`, s.theNodeIsDrainedWithAMaintenanceReason)
	sc.Step(`^the test job is cancelled$`, s.theTestJobIsCancelled)
	sc.Step(`^the old instance is removed$`, s.theOldInstanceIsRemoved)
	sc.Step(`^a replacement node joins the cluster$`, s.aReplacementNodeJoinsTheCluster)
	sc.Step(`^the replacement node passes GPU validation$`, s.theReplacementNodePassesGPUValidation)
}

func (s *NodeReplacement) aTestJobIsSubmittedAndRunningOnAWorkerNode(ctx context.Context) error {
	workers, err := s.slurm.AnyGPUWorkers(1)
	if err != nil {
		return err
	}
	s.replacementWorker = workers[0]

	node, err := s.slurm.NodeInfo(ctx, s.replacementWorker.Name)
	if err != nil {
		return fmt.Errorf("read original node state: %w", err)
	}

	if node.InstanceID == "" {
		return fmt.Errorf("parse InstanceId: no match in %q", node.Raw)
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
	if _, err := s.exec.Jail().Run(ctx, fmt.Sprintf("srun -w %s --gpus-per-node=8 nvidia-smi -L >/dev/null", framework.ShellQuote(workerName))); err != nil {
		node, stateErr := s.slurm.NodeInfo(ctx, workerName)
		if stateErr == nil {
			s.exec.Logf("replacement worker state after failed final validation:\n%s", strings.TrimSpace(node.Raw))
		}
		return fmt.Errorf("validate replacement worker is operational from login node: %w", err)
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
