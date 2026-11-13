package sharedsteps

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type computeInstanceRuntime struct {
	framework.Runtime
	states     map[string][]string
	operations map[string][]string
	calls      []string
}

func (r *computeInstanceRuntime) Local() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		r.calls = append(r.calls, strings.Join(args, " "))
		if len(args) < 6 || args[0] != "nebius" || args[1] != "compute" || args[2] != "instance" {
			return "", errors.New("unexpected command")
		}
		switch {
		case args[3] == "operation" && args[4] == "get":
			return nextReply(r.operations, args[6]), nil
		case args[3] == "get":
			return `{"metadata":{"id":"` + args[5] + `"},"status":{"state":"` + nextReply(r.states, args[5]) + `"}}`, nil
		case args[4] == "--id":
			return `{"id":"op-` + args[3] + `-` + args[5] + `","description":"` + args[3] + `"}`, nil
		}
		return "", errors.New("unexpected command")
	})
}

// nextReply returns the first reply recorded for key and keeps the last one for later calls.
func nextReply(replies map[string][]string, key string) string {
	values := replies[key]
	if len(values) > 1 {
		replies[key] = values[1:]
	}
	return values[0]
}

func (r *computeInstanceRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	var err error
	for range 3 {
		var done bool
		if done, err = condition(ctx); err == nil && done {
			return nil
		}
	}
	return err
}

func TestRequestInstanceActionIsAsync(t *testing.T) {
	runtime := &computeInstanceRuntime{}

	stop, err := requestInstanceAction(t.Context(), runtime, "stop", "computeinstance-1")
	require.NoError(t, err)
	start, err := requestInstanceAction(t.Context(), runtime, "start", "computeinstance-1")
	require.NoError(t, err)

	assert.Equal(t, "op-stop-computeinstance-1", stop)
	assert.Equal(t, "op-start-computeinstance-1", start)
	assert.Equal(t, []string{
		"nebius compute instance stop --id computeinstance-1 --async --format json",
		"nebius compute instance start --id computeinstance-1 --async --format json",
	}, runtime.calls, "Managed runners route this exact nebius command shape to their CLI")
}

func TestParseInstanceState(t *testing.T) {
	state, err := parseInstanceState(`{"metadata":{"id":"i"},"spec":{},"status":{"state":"STOPPED","reconciling":false}}`)
	require.NoError(t, err)
	assert.Equal(t, instanceStateStopped, state)

	_, err = parseInstanceState(`{"status":{}}`)
	assert.ErrorContains(t, err, "status.state is missing")
	_, err = parseInstanceState("not json")
	assert.ErrorContains(t, err, "decode instance JSON")
}

func TestWaitForInstancesStateNamesPendingInstances(t *testing.T) {
	runtime := &computeInstanceRuntime{states: map[string][]string{
		"computeinstance-1": {"STOPPING", "STOPPED"},
		"computeinstance-2": {"STOPPED"},
	}}
	require.NoError(t, waitForInstancesState(t.Context(), runtime, []string{"computeinstance-1", "computeinstance-2"},
		instanceStateStopped, time.Minute, nil))

	runtime = &computeInstanceRuntime{states: map[string][]string{"computeinstance-1": {"STARTING"}}}
	err := waitForInstancesState(t.Context(), runtime, []string{"computeinstance-1"}, instanceStateRunning, time.Minute, nil)
	assert.EqualError(t, err, "instances not RUNNING yet: computeinstance-1=STARTING")
}

func TestStartStoppedInstancesWaitsOutStopAndStartsOnlyStoppedInstances(t *testing.T) {
	runtime := &computeInstanceRuntime{states: map[string][]string{
		"instance-0": {"STOPPING", "STOPPED", "RUNNING"},
		"instance-1": {"RUNNING"},
	}}

	require.NoError(t, startStoppedInstances(t.Context(), runtime, []string{"instance-0", "instance-1"}, time.Minute))

	assert.Equal(t, []string{
		"nebius compute instance get --id instance-0 --format json", "nebius compute instance get --id instance-1 --format json",
		"nebius compute instance get --id instance-0 --format json", "nebius compute instance start --id instance-0 --async --format json",
		"nebius compute instance get --id instance-1 --format json",
		"nebius compute instance get --id instance-0 --format json", "nebius compute instance get --id instance-1 --format json",
	}, runtime.calls, "a stopping instance cannot be started yet, and a running one is left alone")
}

func (*computeInstanceRuntime) Logf(string, ...any) {}

func TestParseAsyncOperationID(t *testing.T) {
	for _, output := range []string{
		"computeoperation-1\n",
		`{"id":"computeoperation-1","description":"Stop Instance","resource_id":"computeinstance-1"}`,
	} {
		id, err := parseAsyncOperationID(output)
		require.NoError(t, err, output)
		assert.Equal(t, "computeoperation-1", id)
	}
	for _, output := range []string{"", "{}", "operation started: computeoperation-1"} {
		_, err := parseAsyncOperationID(output)
		assert.Error(t, err, output)
	}
}

func TestWaitForInstanceOperationsWaitsForEveryOperation(t *testing.T) {
	runtime := &computeInstanceRuntime{operations: map[string][]string{
		"op-1": {`{"id":"op-1"}`, `{"id":"op-1","finished_at":"2026-11-12T10:00:00Z","status":{}}`},
		"op-2": {`{"id":"op-2","finished_at":"2026-11-12T10:00:00Z","status":{}}`},
	}}

	require.NoError(t, waitForInstanceOperations(t.Context(), runtime, []string{"op-1", "op-2"}, time.Minute))

	assert.Equal(t, []string{
		"nebius compute instance operation get --id op-1 --format json",
		"nebius compute instance operation get --id op-2 --format json",
		"nebius compute instance operation get --id op-1 --format json",
	}, runtime.calls, "only the unfinished operation is polled again")
}

func TestWaitForInstanceOperationsFailsOnAFailedOperationAtOnce(t *testing.T) {
	runtime := &computeInstanceRuntime{operations: map[string][]string{
		"op-1": {`{"id":"op-1"}`},
		"op-2": {`{"id":"op-2","description":"Start Instance","resource_id":"computeinstance-2",` +
			`"finished_at":"2026-11-12T10:00:00Z","status":{"code":8,"message":"not enough resources"}}`},
	}}

	err := waitForInstanceOperations(t.Context(), runtime, []string{"op-1", "op-2"}, time.Minute)

	assert.EqualError(t, err, "Compute operation op-2 (Start Instance of computeinstance-2) failed with code 8: not enough resources")
	assert.Len(t, runtime.calls, 2, "a failed operation ends the wait without waiting for the others")
}
