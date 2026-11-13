package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

// Compute instance states reported in status.state.
const (
	instanceStateRunning = "RUNNING"
	instanceStateStopped = "STOPPED"
)

// Compute instances are managed with the local nebius CLI.
// Managed Soperator runners route `nebius <args>` to their own CLI, so keep every call in this shape.

// requestInstanceAction requests an action such as stop or start of a Compute instance without waiting
// and returns the operation ID.
func requestInstanceAction(ctx context.Context, runtime framework.Runtime, action, instanceID string) (operationID string, err error) {
	output, err := runtime.Local().Run(ctx, "nebius", "compute", "instance", action, "--id", instanceID, "--async", "--format", "json")
	if err != nil {
		return "", fmt.Errorf("%s Compute instance %s: %w", action, instanceID, err)
	}
	operationID, err = parseAsyncOperationID(output)
	if err != nil {
		return "", fmt.Errorf("%s Compute instance %s: %w", action, instanceID, err)
	}
	return operationID, nil
}

// parseAsyncOperationID reads the operation ID an --async request prints: the bare ID, which current CLIs
// print even with --format json, or the operation as JSON.
func parseAsyncOperationID(output string) (string, error) {
	var operation computeOperation
	if json.Unmarshal([]byte(output), &operation) == nil && operation.ID != "" {
		return operation.ID, nil
	}
	if fields := strings.Fields(output); len(fields) == 1 && !strings.ContainsAny(fields[0], `{}"`) {
		return fields[0], nil
	}
	return "", fmt.Errorf("read operation ID from async response %q", strings.TrimSpace(output))
}

// computeOperation is a Compute operation as the CLI prints it. A finished operation has finished_at;
// a failed one also has a non-zero status code.
type computeOperation struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	ResourceID  string `json:"resource_id"`
	FinishedAt  string `json:"finished_at"`
	Status      struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
}

// waitForInstanceOperations waits until every Compute instance operation finishes and fails as soon
// as one of them fails, with the error Compute reports.
func waitForInstanceOperations(ctx context.Context, runtime framework.Runtime, operationIDs []string, timeout time.Duration) error {
	var failed error
	finished := make(map[string]bool, len(operationIDs))
	err := runtime.WaitFor(ctx, "Compute instance operations", timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		var pending []string
		for _, id := range operationIDs {
			if finished[id] {
				continue
			}
			output, err := runtime.Local().Run(waitCtx, "nebius", "compute", "instance", "operation", "get", "--id", id, "--format", "json")
			if err != nil {
				return false, fmt.Errorf("get Compute operation %s: %w", id, err)
			}
			var operation computeOperation
			if err := json.Unmarshal([]byte(output), &operation); err != nil {
				return false, fmt.Errorf("decode Compute operation %s: %w", id, err)
			}
			if operation.FinishedAt == "" {
				pending = append(pending, id)
				continue
			}
			finished[id] = true
			if operation.Status.Code != 0 {
				failed = fmt.Errorf("Compute operation %s (%s of %s) failed with code %d: %s",
					id, operation.Description, operation.ResourceID, operation.Status.Code, operation.Status.Message)
				return true, nil
			}
		}
		if len(pending) > 0 {
			return false, fmt.Errorf("operations not finished yet: %s", strings.Join(pending, ", "))
		}
		return true, nil
	})
	if failed != nil {
		return failed
	}
	return err
}

// instanceState returns the status.state of a Compute instance, for example RUNNING or STOPPED.
func instanceState(ctx context.Context, runtime framework.Runtime, instanceID string) (string, error) {
	output, err := runtime.Local().Run(ctx, "nebius", "compute", "instance", "get", "--id", instanceID, "--format", "json")
	if err != nil {
		return "", fmt.Errorf("get Compute instance %s: %w", instanceID, err)
	}
	state, err := parseInstanceState(output)
	if err != nil {
		return "", fmt.Errorf("get Compute instance %s: %w", instanceID, err)
	}
	return state, nil
}

func parseInstanceState(output string) (string, error) {
	var instance struct {
		Status struct {
			State string `json:"state"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &instance); err != nil {
		return "", fmt.Errorf("decode instance JSON: %w", err)
	}
	if instance.Status.State == "" {
		return "", fmt.Errorf("decode instance JSON: status.state is missing")
	}
	return instance.Status.State, nil
}

// startStoppedInstances brings every instance back to RUNNING: it starts each instance it sees
// STOPPED and waits out a stop in progress first, so it is safe whatever state a failure left.
func startStoppedInstances(ctx context.Context, runtime framework.Runtime, instanceIDs []string, timeout time.Duration) error {
	return waitForInstancesState(ctx, runtime, instanceIDs, instanceStateRunning, timeout,
		func(ctx context.Context, id, state string) error {
			if state != instanceStateStopped {
				return nil
			}
			runtime.Logf("Requesting start of Compute instance %s", id)
			_, err := requestInstanceAction(ctx, runtime, "start", id)
			return err
		})
}

// waitForInstancesState polls every instance until all report the state and names the instances
// still in another state when the wait times out. visit, when set, sees each such instance and may act on it.
func waitForInstancesState(
	ctx context.Context,
	runtime framework.Runtime,
	instanceIDs []string,
	state string,
	timeout time.Duration,
	visit func(ctx context.Context, id, state string) error,
) error {
	description := fmt.Sprintf("Compute instances %s", state)
	return runtime.WaitFor(ctx, description, timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		var pending []string
		for _, id := range instanceIDs {
			current, err := instanceState(waitCtx, runtime, id)
			if err != nil {
				return false, err
			}
			if current == state {
				continue
			}
			if visit != nil {
				if err := visit(waitCtx, id, current); err != nil {
					return false, err
				}
			}
			pending = append(pending, id+"="+current)
		}
		if len(pending) > 0 {
			slices.Sort(pending)
			return false, fmt.Errorf("instances not %s yet: %s", state, strings.Join(pending, ", "))
		}
		return true, nil
	})
}
