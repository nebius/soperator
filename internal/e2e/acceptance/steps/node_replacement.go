package steps

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

var (
	instanceIDPattern = regexp.MustCompile(`InstanceId=([^\s]+)`)
	reasonPattern     = regexp.MustCompile(`Reason=([^\n]+)`)
)

type NodeReplacement struct {
	exec framework.Executor
}

func NewNodeReplacement(_ *framework.SharedState, exec framework.Executor) NodeReplacement {
	return NodeReplacement{exec: exec}
}

func (s NodeReplacement) Register(sc *godog.ScenarioContext) {
	sc.Step(`^a maintenance event replaces the worker node and returns it to service$`, s.aMaintenanceEventReplacesTheWorkerNodeAndReturnsItToService)
}

func (s NodeReplacement) aMaintenanceEventReplacesTheWorkerNodeAndReturnsItToService(ctx context.Context) error {
	worker, err := s.exec.AnyWorker()
	if err != nil {
		return err
	}
	workerName := worker.Name

	nodeState, err := s.exec.ExecController(ctx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(workerName)))
	if err != nil {
		return fmt.Errorf("read original node state: %w", err)
	}

	originalInstanceID := parseInstanceID(nodeState)
	if originalInstanceID == "" {
		return fmt.Errorf("unable to parse InstanceId from %q", nodeState)
	}

	maintenanceJobID, err := s.exec.ExecJail(ctx, fmt.Sprintf("sbatch --parsable -w %s --job-name=e2e-node-replacement --wrap=%s", framework.ShellQuote(workerName), framework.ShellQuote("sleep 600")))
	if err != nil {
		return fmt.Errorf("submit maintenance job: %w", err)
	}
	maintenanceJobID = strings.TrimSpace(maintenanceJobID)
	if maintenanceJobID == "" {
		return fmt.Errorf("empty maintenance job id")
	}
	defer func() {
		if err := s.cancelJob(context.Background(), maintenanceJobID); err != nil {
			s.exec.Logf("cleanup: cancel maintenance job: %v", err)
		}
	}()

	if err := s.exec.WaitFor(ctx, "maintenance job running", 25*time.Minute, 10*time.Second, func(waitCtx context.Context) (bool, error) {
		status, err := s.exec.ExecController(waitCtx, fmt.Sprintf("squeue -h -j %s -o '%%T'", framework.ShellQuote(maintenanceJobID)))
		if err != nil {
			return false, err
		}
		return strings.Contains(status, "RUNNING"), nil
	}); err != nil {
		return err
	}

	patch := fmt.Sprintf(`{"status":{"conditions":[{"type":"NebiusMaintenanceScheduled","status":"True","reason":"AcceptanceTest","message":"Maintenance scheduled for node","lastTransitionTime":"%s"}]}}`, time.Now().UTC().Format(time.RFC3339))
	if _, err := s.exec.Run(ctx, "kubectl", "patch", "node", originalInstanceID, "--subresource=status", "--type=strategic", "-p", patch); err != nil {
		return fmt.Errorf("patch maintenance condition: %w", err)
	}

	if err := s.exec.WaitFor(ctx, "node drain reason", 25*time.Minute, 15*time.Second, func(waitCtx context.Context) (bool, error) {
		state, err := s.exec.ExecController(waitCtx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(workerName)))
		if err != nil {
			return false, err
		}

		reason := parseReason(state)
		return strings.Contains(state, "DRAIN") && strings.HasPrefix(reason, "[compute_maintenance]"), nil
	}); err != nil {
		return err
	}

	if err := s.cancelJob(ctx, maintenanceJobID); err != nil {
		return err
	}
	maintenanceJobID = ""

	if err := s.exec.WaitFor(ctx, "old instance removal", 25*time.Minute, 30*time.Second, func(waitCtx context.Context) (bool, error) {
		_, err := s.exec.Run(waitCtx, "nebius", "compute", "instance", "get", "--id", originalInstanceID, "--format", "json")
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}); err != nil {
		return err
	}

	if err := s.exec.WaitFor(ctx, "replacement node ready", 25*time.Minute, 60*time.Second, func(waitCtx context.Context) (bool, error) {
		state, err := s.exec.ExecController(waitCtx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(workerName)))
		if err != nil {
			return false, err
		}

		newInstanceID := parseInstanceID(state)
		if newInstanceID == "" || newInstanceID == originalInstanceID || strings.Contains(state, "DRAIN") {
			return false, nil
		}

		return true, nil
	}); err != nil {
		return err
	}

	if _, err := s.exec.ExecJail(ctx, fmt.Sprintf("srun -w %s nvidia-smi -L >/dev/null", framework.ShellQuote(workerName))); err != nil {
		output, stateErr := s.exec.ExecController(ctx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(workerName)))
		if stateErr == nil {
			s.exec.Logf("replacement worker state after failed final validation:\n%s", strings.TrimSpace(output))
		}
		return fmt.Errorf("validate replacement worker is operational from login node: %w", err)
	}

	return nil
}

func (s NodeReplacement) cancelJob(ctx context.Context, maintenanceJobID string) error {
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
