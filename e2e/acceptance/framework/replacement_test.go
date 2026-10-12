package framework

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type replacementRuntime struct {
	Runtime
	instanceLookups []error
	nodeStates      []string
}

func (r *replacementRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	for range 4 {
		ready, err := condition(ctx)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
	}
	return errors.New("timed out")
}

func (r *replacementRuntime) Local() ArgsScope {
	return NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		if len(args) != 8 || args[0] != "nebius" || args[4] != "--id" {
			return "", fmt.Errorf("unexpected command: %v", args)
		}
		err := r.instanceLookups[0]
		r.instanceLookups = r.instanceLookups[1:]
		return "", err
	})
}

func (r *replacementRuntime) Controller() CommandScope {
	return NewCommandScope(func(_ context.Context, command string) (string, error) {
		if command != "scontrol show node 'worker-0'" {
			return "", fmt.Errorf("unexpected command: %s", command)
		}
		state := r.nodeStates[0]
		r.nodeStates = r.nodeStates[1:]
		return state, nil
	})
}

func TestWaitForInstanceRemoval(t *testing.T) {
	runtime := &replacementRuntime{instanceLookups: []error{nil, errors.New("instance not found")}}
	require.NoError(t, WaitForInstanceRemoval(t.Context(), runtime, "old-instance", time.Minute, time.Second))
}

func TestWaitForInstanceRemovalPropagatesLookupError(t *testing.T) {
	runtime := &replacementRuntime{instanceLookups: []error{errors.New("permission denied")}}
	err := WaitForInstanceRemoval(t.Context(), runtime, "old-instance", time.Minute, time.Second)
	require.ErrorContains(t, err, "permission denied")
}

func TestWaitForUsableReplacement(t *testing.T) {
	runtime := &replacementRuntime{nodeStates: []string{
		"NodeName=worker-0 InstanceId=old-instance State=IDLE",
		"NodeName=worker-0 InstanceId=new-instance State=IDLE+DRAIN",
		"NodeName=worker-0 InstanceId=new-instance State=IDLE",
	}}
	err := WaitForUsableReplacement(t.Context(), runtime, NewSlurmClient(runtime), "worker-0", "old-instance", time.Minute, time.Second)
	require.NoError(t, err)
	require.Empty(t, runtime.nodeStates)
}
