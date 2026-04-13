package steps

import (
	"context"
	"fmt"
	"path"
	"regexp"
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

var (
	instanceIDPattern = regexp.MustCompile(`InstanceId=([^\s]+)`)
	reasonPattern     = regexp.MustCompile(`Reason=([^\n]+)`)
)

type NodeReplacement struct {
	exec               framework.Exec
	replacementWorker  framework.WorkerRef
	originalInstanceID string
	maintenanceJobID   string
}

func NewNodeReplacement(exec framework.Exec) *NodeReplacement {
	return &NodeReplacement{exec: exec}
}

func (s *NodeReplacement) Register(sc *godog.ScenarioContext) {
	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		if path.Base(scenario.Uri) != nodeReplacementFeatureFile || s.maintenanceJobID == "" {
			return ctx, nil
		}
		if cancelErr := s.cancelJob(context.Background(), s.maintenanceJobID); cancelErr != nil {
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
	worker, err := s.exec.AnyGPUWorker()
	if err != nil {
		return err
	}
	s.replacementWorker = worker

	nodeState, err := s.exec.ExecController(ctx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(worker.Name)))
	if err != nil {
		return fmt.Errorf("read original node state: %w", err)
	}

	originalInstanceID := parseInstanceID(nodeState)
	if originalInstanceID == "" {
		return fmt.Errorf("parse InstanceId: no match in %q", nodeState)
	}
	s.originalInstanceID = originalInstanceID

	jobID, err := s.exec.ExecJail(ctx, fmt.Sprintf(
		"sbatch --parsable -w %s --job-name=e2e-node-replacement --wrap=%s",
		framework.ShellQuote(worker.Name), framework.ShellQuote("sleep 600")))
	if err != nil {
		return fmt.Errorf("submit maintenance job: %w", err)
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return fmt.Errorf("empty maintenance job id")
	}
	s.maintenanceJobID = jobID

	return s.exec.WaitFor(ctx, "maintenance job running", nodeReplacementJobTimeout, 10*time.Second, func(waitCtx context.Context) (bool, error) {
		status, err := s.exec.ExecController(waitCtx, fmt.Sprintf("squeue -h -j %s -o '%%T'", framework.ShellQuote(jobID)))
		if err != nil {
			return false, err
		}
		return strings.Contains(status, "RUNNING"), nil
	})
}

func (s *NodeReplacement) aMaintenanceEventIsTriggeredForThatNode(ctx context.Context) error {
	patch := fmt.Sprintf(
		`{"status":{"conditions":[{"type":"NebiusMaintenanceScheduled","status":"True","reason":"AcceptanceTest","message":"Maintenance scheduled for node","lastTransitionTime":"%s"}]}}`,
		time.Now().UTC().Format(time.RFC3339))
	if _, err := s.exec.Run(ctx, "kubectl", "patch", "node", s.originalInstanceID,
		"--subresource=status", "--type=strategic", "-p", patch); err != nil {
		return fmt.Errorf("patch maintenance condition: %w", err)
	}
	return nil
}

func (s *NodeReplacement) theNodeIsDrainedWithAMaintenanceReason(ctx context.Context) error {
	workerName := s.replacementWorker.Name
	return s.exec.WaitFor(ctx, "node drain reason", nodeReplacementDrainTimeout, 15*time.Second, func(waitCtx context.Context) (bool, error) {
		state, err := s.exec.ExecController(waitCtx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(workerName)))
		if err != nil {
			return false, err
		}
		reason := parseReason(state)
		return strings.Contains(state, "DRAIN") && strings.HasPrefix(reason, "[compute_maintenance]"), nil
	})
}

func (s *NodeReplacement) theTestJobIsCancelled(ctx context.Context) error {
	if err := s.cancelJob(ctx, s.maintenanceJobID); err != nil {
		return err
	}
	s.maintenanceJobID = ""
	return nil
}

func (s *NodeReplacement) theOldInstanceIsRemoved(ctx context.Context) error {
	originalInstanceID := s.originalInstanceID
	return s.exec.WaitFor(ctx, "old instance removal", nodeReplacementRemoveTimeout, 30*time.Second, func(waitCtx context.Context) (bool, error) {
		_, err := s.exec.Run(waitCtx, "nebius", "compute", "instance", "get", "--id", originalInstanceID, "--format", "json")
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
		state, err := s.exec.ExecController(waitCtx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(workerName)))
		if err != nil {
			return false, err
		}

		newInstanceID := parseInstanceID(state)
		if newInstanceID == "" || newInstanceID == originalInstanceID || strings.Contains(state, "DRAIN") {
			return false, nil
		}
		return true, nil
	})
}

func (s *NodeReplacement) theReplacementNodePassesGPUValidation(ctx context.Context) error {
	workerName := s.replacementWorker.Name
	if _, err := s.exec.ExecJail(ctx, fmt.Sprintf("srun -w %s nvidia-smi -L >/dev/null", framework.ShellQuote(workerName))); err != nil {
		output, stateErr := s.exec.ExecController(ctx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(workerName)))
		if stateErr == nil {
			s.exec.Logf("replacement worker state after failed final validation:\n%s", strings.TrimSpace(output))
		}
		return fmt.Errorf("validate replacement worker is operational from login node: %w", err)
	}
	return nil
}

func (s *NodeReplacement) cancelJob(ctx context.Context, maintenanceJobID string) error {
	if maintenanceJobID == "" {
		return nil
	}

	if _, err := s.exec.ExecController(ctx, fmt.Sprintf("scancel %s || true", framework.ShellQuote(maintenanceJobID))); err != nil {
		return fmt.Errorf("cancel maintenance job %s: %w", maintenanceJobID, err)
	}
	return nil
}

func parseInstanceID(state string) string {
	match := instanceIDPattern.FindStringSubmatch(state)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func parseReason(state string) string {
	match := reasonPattern.FindStringSubmatch(state)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}
