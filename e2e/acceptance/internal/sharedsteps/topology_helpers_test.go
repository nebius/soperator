package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestExpandSlurmHostlistCachesExpandedValues(t *testing.T) {
	runtime := &topologyHostlistRuntime{outputs: map[string]string{
		"scontrol show hostnames 'worker-[0-1]'": "worker-0\nworker-1\n",
	}}
	cache := make(map[string][]string)

	first, err := expandSlurmHostlist(t.Context(), runtime, "worker-[0-1]", cache)
	require.NoError(t, err)
	second, err := expandSlurmHostlist(t.Context(), runtime, "worker-[0-1]", cache)
	require.NoError(t, err)

	assert.Equal(t, []string{"worker-0", "worker-1"}, first)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, runtime.calls)
}

func TestExpandSlurmHostlistParsesPlainValuesLocally(t *testing.T) {
	runtime := &topologyHostlistRuntime{}

	nodes, err := expandSlurmHostlist(t.Context(), runtime, "worker-0,worker-1", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"worker-0", "worker-1"}, nodes)
	assert.Zero(t, runtime.calls)
}

type topologyHostlistRuntime struct {
	framework.Runtime
	outputs map[string]string
	calls   int
}

func (r *topologyHostlistRuntime) Controller() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		r.calls++
		output, found := r.outputs[command]
		if !found {
			return "", fmt.Errorf("unexpected controller command: %s", strings.TrimSpace(command))
		}
		return output, nil
	})
}
