package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

func TestListClusterNodeSetsFiltersAndSorts(t *testing.T) {
	exec := &nodeSetHelperExec{outputs: map[string]string{
		"get\x00nodesets\x00-n\x00soperator\x00-o\x00json": `{
			"items": [
				{"metadata":{"name":"worker-z","namespace":"soperator"},"spec":{"clusterName":"soperator"}},
				{"metadata":{"name":"other","namespace":"soperator"},"spec":{"clusterName":"other"}},
				{"metadata":{"name":"worker-a","namespace":"soperator"},"spec":{}}
			]
		}`,
	}}

	nodeSets, err := listClusterNodeSets(t.Context(), framework.NewKubectlClient(exec), "soperator")
	require.NoError(t, err)
	require.Len(t, nodeSets, 2)
	assert.Equal(t, "worker-a", nodeSets[0].Metadata.Name)
	assert.Equal(t, "worker-z", nodeSets[1].Metadata.Name)
}

func TestNodeSetWorkerPodsUsesSharedSelector(t *testing.T) {
	exec := &nodeSetHelperExec{outputs: map[string]string{
		"get\x00pods\x00-n\x00soperator\x00-l\x00slurm.nebius.ai/nodeset=worker-a\x00-o\x00json": `{
			"items": [{"metadata":{"name":"worker-a-0","namespace":"soperator"}}]
		}`,
	}}

	pods, err := nodeSetWorkerPods(t.Context(), framework.NewKubectlClient(exec), kubeobjects.NodeSet{
		Metadata: kubeobjects.ObjectMeta{Name: "worker-a", Namespace: "soperator"},
	})
	require.NoError(t, err)
	require.Len(t, pods, 1)
	assert.Equal(t, "worker-a-0", pods[0].Name)
}

type nodeSetHelperExec struct {
	framework.Exec
	outputs map[string]string
}

func (e *nodeSetHelperExec) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		command := strings.Join(args, "\x00")
		output, ok := e.outputs[command]
		if !ok {
			return "", fmt.Errorf("unexpected kubectl command: %s", strings.Join(args, " "))
		}
		return output, nil
	})
}
