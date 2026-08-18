package framework

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKubectlClientNodeSetsFiltersByClusterName(t *testing.T) {
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00nodesets\x00-n\x00soperator\x00-o\x00json": `{
			"items": [
				{
					"metadata": {"name": "worker-gpu", "namespace": "soperator"},
					"spec": {"clusterName": "soperator", "replicas": 2, "gpu": {"enabled": true}},
					"status": {"phase": "Ready", "replicas": 1}
				},
				{
					"metadata": {"name": "worker-cpu", "namespace": "soperator"},
					"spec": {"clusterName": "soperator", "replicas": 3, "gpu": {"enabled": false}},
					"status": {"phase": "Ready", "replicas": 3}
				},
				{
					"metadata": {"name": "other-worker", "namespace": "soperator"},
					"spec": {"clusterName": "other", "replicas": 4, "gpu": {"enabled": true}},
					"status": {"phase": "Ready", "replicas": 4}
				}
			]
		}`,
	}}

	nodeSets, err := NewKubectlClient(exec).NodeSets(t.Context(), "soperator")
	require.NoError(t, err)
	require.Len(t, nodeSets, 2)

	assert.Equal(t, NodeSetInfo{
		Name:          "worker-cpu",
		Namespace:     "soperator",
		Replicas:      3,
		ReadyReplicas: 3,
		Phase:         "Ready",
	}, nodeSets[0])
	assert.Equal(t, NodeSetInfo{
		Name:          "worker-gpu",
		Namespace:     "soperator",
		Replicas:      2,
		ReadyReplicas: 1,
		HasGPU:        true,
		Phase:         "Ready",
	}, nodeSets[1])
}

func TestKubectlClientNodeSetsKeepsLegacyMissingClusterName(t *testing.T) {
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00nodesets\x00-n\x00soperator\x00-o\x00json": `{
			"items": [
				{"metadata": {"name": "worker-gpu"}, "spec": {"clusterName": "soperator", "replicas": 2}},
				{"metadata": {"name": "worker-cpu"}, "spec": {"replicas": 3}}
			]
		}`,
	}}

	nodeSets, err := NewKubectlClient(exec).NodeSets(t.Context(), "soperator")
	require.NoError(t, err)
	require.Len(t, nodeSets, 2)
	assert.Equal(t, "worker-cpu", nodeSets[0].Name)
	assert.Equal(t, "worker-gpu", nodeSets[1].Name)
}

func TestKubectlClientWorkerPods(t *testing.T) {
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00pods\x00-n\x00soperator\x00-l\x00slurm.nebius.ai/worker=true\x00-o\x00json": `{
			"items": [
				{
					"metadata": {"name": "kube-worker-a"},
					"spec": {"hostname": "worker-a", "nodeName": "node-a"},
					"status": {"conditions": [{"type": "Ready", "status": "True"}]}
				},
				{
					"metadata": {"name": "kube-worker-b"},
					"spec": {"hostname": "worker-b", "nodeName": "node-b"},
					"status": {"conditions": [{"type": "Ready", "status": "False"}]}
				}
			]
		}`,
	}}

	pods, err := NewKubectlClient(exec).WorkerPods(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []WorkerPodInfo{
		{SlurmNodeName: "worker-a", PodName: "kube-worker-a", KubernetesNodeName: "node-a", Ready: true},
		{SlurmNodeName: "worker-b", PodName: "kube-worker-b", KubernetesNodeName: "node-b"},
	}, pods)
}

func TestKubectlClientWorkerPodsRejectsMissingHostname(t *testing.T) {
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00pods\x00-n\x00soperator\x00-l\x00slurm.nebius.ai/worker=true\x00-o\x00json": `{
			"items": [{"metadata": {"name": "worker-0"}}]
		}`,
	}}

	_, err := NewKubectlClient(exec).WorkerPods(t.Context())
	require.Error(t, err)
	assert.ErrorContains(t, err, "empty spec.hostname")
}

func TestKubectlClientWorkerPodsRejectsDuplicateHostname(t *testing.T) {
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00pods\x00-n\x00soperator\x00-l\x00slurm.nebius.ai/worker=true\x00-o\x00json": `{
			"items": [
				{"metadata": {"name": "worker-a-0"}, "spec": {"hostname": "worker-0"}},
				{"metadata": {"name": "worker-b-0"}, "spec": {"hostname": "worker-0"}}
			]
		}`,
	}}

	_, err := NewKubectlClient(exec).WorkerPods(t.Context())
	require.Error(t, err)
	assert.ErrorContains(t, err, "both declare spec.hostname=worker-0")
}

type kubectlClientTestExec struct {
	kubectl map[string]string
}

func (e *kubectlClientTestExec) Kubectl() ArgsScope {
	return NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		key := strings.Join(args, "\x00")
		out, ok := e.kubectl[key]
		if !ok {
			return "", fmt.Errorf("unexpected kubectl args: %s", strings.Join(args, " "))
		}
		return out, nil
	})
}

func (e *kubectlClientTestExec) Local() ArgsScope {
	return NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		return "", fmt.Errorf("unexpected local command")
	})
}

func (e *kubectlClientTestExec) Controller() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		return "", fmt.Errorf("unexpected controller command: %s", command)
	})
}

func (e *kubectlClientTestExec) Jail() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		return "", fmt.Errorf("unexpected jail command: %s", command)
	})
}

func (e *kubectlClientTestExec) Worker(worker WorkerInfo) CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		return "", fmt.Errorf("unexpected worker command: %s", command)
	})
}

func (e *kubectlClientTestExec) WorkerPod(pod WorkerPodInfo) CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		return "", fmt.Errorf("unexpected worker pod command: %s", command)
	})
}

func (e *kubectlClientTestExec) WaitFor(ctx context.Context, description string, timeout, pollInterval time.Duration, condition func(context.Context) (bool, error)) error {
	return fmt.Errorf("unexpected wait: %s", description)
}

func (e *kubectlClientTestExec) Logf(format string, args ...any) {}
