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

func TestHostlistExpanderCachesExpandedValues(t *testing.T) {
	runtime := &hostlistRuntime{outputs: map[string]string{
		"scontrol show hostnames 'worker-[0-1]'": "worker-0\nworker-1\n",
	}}
	hostlists := newCachedHostlistExpander(runtime)

	first, err := hostlists.expand(t.Context(), "worker-[0-1]")
	require.NoError(t, err)
	second, err := hostlists.expand(t.Context(), "worker-[0-1]")
	require.NoError(t, err)

	assert.Equal(t, []string{"worker-0", "worker-1"}, first)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, runtime.calls)
}

func TestHostlistExpanderParsesPlainValuesLocally(t *testing.T) {
	runtime := &hostlistRuntime{}

	nodes, err := newCachedHostlistExpander(runtime).expand(t.Context(), "worker-0,worker-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"worker-0", "worker-1"}, nodes)
	assert.Zero(t, runtime.calls)
}

func TestPlainHostlistDefersRangesToScontrol(t *testing.T) {
	nodes, ok := plainHostlist("worker-0,worker-1")
	assert.True(t, ok)
	assert.Equal(t, []string{"worker-0", "worker-1"}, nodes)

	nodes, ok = plainHostlist("(null)")
	assert.True(t, ok)
	assert.Nil(t, nodes)

	_, ok = plainHostlist("worker-[0-2]")
	assert.False(t, ok)
}

type hostlistRuntime struct {
	framework.Runtime
	outputs map[string]string
	calls   int
}

func (r *hostlistRuntime) Controller() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		r.calls++
		output, found := r.outputs[command]
		if !found {
			return "", fmt.Errorf("unexpected controller command: %s", strings.TrimSpace(command))
		}
		return output, nil
	})
}
