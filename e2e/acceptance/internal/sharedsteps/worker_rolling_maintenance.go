package sharedsteps

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const workerRollingUpdateDrainReason = "soperator rolling update"

func (s *WorkerRollingUpdate) registerMaintenanceSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a maintenance event is triggered for the (first|second) worker$`, s.triggerMaintenance)
	sc.Step(`^the (first|second) worker is drained with a (maintenance|rolling update) reason$`, s.assertWorkerDrain)
}

func (s *WorkerRollingUpdate) triggerMaintenance(ctx context.Context, worker string) error {
	target, err := s.maintenanceTarget(worker)
	if err != nil {
		return err
	}
	if target.replacement == nil {
		replacement := NewNodeReplacement(s.runtime, s.slurm, s.selector)
		if err := replacement.selectWorker(ctx, target.worker); err != nil {
			return err
		}
		target.replacement = replacement
	}
	return target.replacement.aMaintenanceEventIsTriggeredForThatNode(ctx)
}

func (s *WorkerRollingUpdate) maintenanceTarget(worker string) (*workerRolloutTarget, error) {
	if len(s.targets) != 2 {
		return nil, fmt.Errorf("select %s maintenance worker: start jobs on two workers first", worker)
	}
	var index int
	switch worker {
	case "first":
	case "second":
		index = 1
	default:
		return nil, fmt.Errorf("select maintenance worker: unknown worker %q", worker)
	}
	return s.targets[index], nil
}

func (s *WorkerRollingUpdate) assertWorkerDrain(ctx context.Context, worker, reason string) error {
	target, err := s.maintenanceTarget(worker)
	if err != nil {
		return err
	}
	var reasonMatches func(string) bool
	timeout := workerRolloutDrainTimeout
	switch reason {
	case "maintenance":
		reasonMatches = maintenanceReasonMatches
		timeout = nodeReplacementDrainTimeout
	case "rolling update":
		reasonMatches = func(actual string) bool {
			actual = strings.TrimSpace(actual)
			return actual == workerRollingUpdateDrainReason || strings.HasPrefix(actual, workerRollingUpdateDrainReason+" ")
		}
		if s.mk8s.groupID != "" {
			timeout = workerMK8sTimeout
		}
	default:
		return fmt.Errorf("check worker drain: unknown reason %q", reason)
	}
	return s.runtime.WaitFor(ctx, worker+" worker drains for "+reason, timeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			node, err := s.slurm.NodeInfoOnce(waitCtx, target.worker.Name)
			if err != nil {
				return false, err
			}
			if !node.HasStateFlag("DRAIN") || !reasonMatches(node.Reason) {
				return false, fmt.Errorf("worker %s %s drain pending: state=%s reason=%q", target.worker.Name, reason, node.State, node.Reason)
			}
			return true, nil
		})
}
