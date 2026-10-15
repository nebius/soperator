package framework

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestKubectlClientNodeIfPresent(t *testing.T) {
	command := "get\x00node\x00worker-host\x00--ignore-not-found\x00-o\x00json"
	for _, tt := range []struct {
		name      string
		output    string
		queryErr  error
		present   bool
		wantError string
	}{
		{name: "present", output: `{"metadata":{"name":"worker-host","uid":"host-uid"}}`, present: true},
		{name: "absent"},
		{name: "whitespace means absent", output: " \n\t"},
		{name: "permission denied", queryErr: errors.New("forbidden"), wantError: "read Kubernetes host worker-host: forbidden"},
		{name: "malformed response", output: "{", wantError: "decode Kubernetes host worker-host"},
		{name: "missing identity", output: `{}`, wantError: "identify Kubernetes host worker-host"},
		{name: "missing name", output: `{"metadata":{"uid":"host-uid"}}`, wantError: "identify Kubernetes host worker-host"},
		{name: "missing UID", output: `{"metadata":{"name":"worker-host"}}`, wantError: "identify Kubernetes host worker-host"},
		{name: "wrong name", output: `{"metadata":{"name":"other-host","uid":"host-uid"}}`, wantError: "observed name=other-host UID=host-uid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			exec := &kubectlClientTestExec{
				kubectl:       map[string]string{command: tt.output},
				kubectlErrors: map[string]error{command: tt.queryErr},
			}
			node, present, err := NewKubectlClient(exec).NodeIfPresent(t.Context(), "worker-host")
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
			assert.Equal(t, tt.present, present)
			assert.Equal(t, 1, exec.kubectlCalls, "query must not retry inside a caller's polling loop")
			if tt.present {
				assert.Equal(t, "worker-host", node.Name)
				assert.Equal(t, "host-uid", string(node.UID))
			} else {
				assert.Equal(t, corev1.Node{}, node)
			}
		})
	}
}

func TestKubectlClientNodeIfPresentRejectsEmptyName(t *testing.T) {
	exec := &kubectlClientTestExec{}
	_, present, err := NewKubectlClient(exec).NodeIfPresent(t.Context(), " \t")
	require.ErrorContains(t, err, "Kubernetes node name is empty")
	assert.False(t, present)
	assert.Zero(t, exec.kubectlCalls)
}

func TestKubectlClientSlurmCluster(t *testing.T) {
	customConfig := "JobRequeue=0"
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00slurmcluster\x00soperator\x00-n\x00soperator\x00-o\x00json": `{
			"metadata": {"name": "soperator", "namespace": "soperator"},
			"spec": {
				"customSlurmConfig": "JobRequeue=0",
				"pamSlurmAdopt": {"enabled": true},
				"slurmNodes": {
					"accounting": {"enabled": true},
					"login": {
						"customInitContainers": [{"name": "tailscale"}],
						"docker": {"enabled": true},
						"userIsolation": {"enabled": true},
						"volumes": {"jailSubMounts": [
							{"name": "image-storage", "mountPath": "/mnt/local-data", "volumeClaimTemplateSpec": {"accessModes": ["ReadWriteOnce"]}}
						]}
					}
				}
			}
		}`,
	}}

	cluster, err := NewKubectlClient(exec).SlurmCluster(t.Context(), "soperator")
	require.NoError(t, err)
	assert.Equal(t, SlurmClusterInfo{
		Name:                      "soperator",
		Namespace:                 "soperator",
		AccountingEnabled:         true,
		LoginDockerEnabled:        true,
		LoginTailscaleEnabled:     true,
		LoginUserIsolationEnabled: true,
		LoginJailSubMounts: []JailSubMountInfo{
			{Name: "image-storage", MountPath: "/mnt/local-data", UsesVolumeClaimTemplate: true},
		},
		PAMSlurmAdoptEnabled: true,
		CustomSlurmConfig:    &customConfig,
	}, cluster)
}

func TestKubectlClientSlurmClusterPAMSlurmAdoptDisabled(t *testing.T) {
	for name, spec := range map[string]string{
		"explicitly disabled": `"pamSlurmAdopt": {"enabled": false}`,
		"omitted":             ``,
	} {
		t.Run(name, func(t *testing.T) {
			exec := &kubectlClientTestExec{kubectl: map[string]string{
				"get\x00slurmcluster\x00soperator\x00-n\x00soperator\x00-o\x00json": fmt.Sprintf(`{
					"metadata": {"name": "soperator", "namespace": "soperator"},
					"spec": {%s}
				}`, spec),
			}}

			cluster, err := NewKubectlClient(exec).SlurmCluster(t.Context(), "soperator")
			require.NoError(t, err)
			assert.False(t, cluster.PAMSlurmAdoptEnabled)
		})
	}
}

func TestKubectlClientReadyWorkloadPodName(t *testing.T) {
	selector := "app.kubernetes.io/instance=soperator,app.kubernetes.io/component=login"
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00pods\x00-n\x00soperator\x00-l\x00" + selector + "\x00-o\x00json": `{
			"items": [
				{"metadata": {"name": "soperator-login-1"}, "status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}},
				{"metadata": {"name": "soperator-login-0"}, "status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}},
				{"metadata": {"name": "login-0"}, "status": {"phase": "Pending", "conditions": [{"type": "Ready", "status": "False"}]}}
			]
		}`,
	}}

	podName, err := NewKubectlClient(exec).ReadyWorkloadPodName(t.Context(), "soperator", "login")
	require.NoError(t, err)
	assert.Equal(t, "soperator-login-0", podName)
}

func TestKubectlClientReadyWorkloadPodNameSupportsUnprefixedPods(t *testing.T) {
	selector := "app.kubernetes.io/instance=soperator,app.kubernetes.io/component=controller"
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00pods\x00-n\x00soperator\x00-l\x00" + selector + "\x00-o\x00json": `{
			"items": [
				{"metadata": {"name": "controller-0"}, "status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}}
			]
		}`,
	}}

	podName, err := NewKubectlClient(exec).ReadyWorkloadPodName(t.Context(), "soperator", "controller")
	require.NoError(t, err)
	assert.Equal(t, "controller-0", podName)
}

func TestKubectlClientSlurmClusterRejectsEmptyName(t *testing.T) {
	_, err := NewKubectlClient(&kubectlClientTestExec{}).SlurmCluster(t.Context(), " ")
	assert.ErrorContains(t, err, "name is empty")
}

func TestKubectlClientPatchSlurmClusterCustomConfig(t *testing.T) {
	for name, test := range map[string]struct {
		value *string
		patch string
	}{
		"set": {
			value: ptrTo("JobRequeue=0\n"),
			patch: `{"spec":{"customSlurmConfig":"JobRequeue=0\n"}}`,
		},
		"remove": {
			patch: `{"spec":{"customSlurmConfig":null}}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			exec := &kubectlClientTestExec{kubectl: map[string]string{
				strings.Join([]string{
					"patch", "slurmcluster", "soperator", "-n", "soperator",
					"--type=merge", "-p", test.patch,
				}, "\x00"): "",
			}}

			err := NewKubectlClient(exec).PatchSlurmClusterCustomConfig(t.Context(), "soperator", test.value)
			require.NoError(t, err)
		})
	}
}

func TestKubectlClientPatchSlurmClusterCustomConfigRejectsEmptyName(t *testing.T) {
	err := NewKubectlClient(&kubectlClientTestExec{}).PatchSlurmClusterCustomConfig(t.Context(), " ", nil)
	assert.ErrorContains(t, err, "name is empty")
}

func ptrTo[T any](value T) *T {
	return &value
}

func TestKubectlClientNodeSetsFiltersByClusterName(t *testing.T) {
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00nodesets\x00-n\x00soperator\x00-o\x00json": `{
			"items": [
				{
					"metadata": {"name": "worker-gpu", "namespace": "soperator"},
					"spec": {
						"clusterName": "soperator",
						"replicas": 2,
						"gpu": {"enabled": true},
						"slurmd": {"volumes": {"jailSubMounts": [
							{"name": "local-data", "mountPath": "/scratch", "volumeClaimTemplateSpec": {"accessModes": ["ReadWriteOnce"]}},
							{"name": "config", "mountPath": "/etc/config", "readOnly": true, "volumeSource": {"configMap": {"name": "config"}}}
						]}}
					},
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
		JailSubMounts: []JailSubMountInfo{
			{Name: "local-data", MountPath: "/scratch", UsesVolumeClaimTemplate: true},
			{Name: "config", MountPath: "/etc/config", ReadOnly: true},
		},
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

func TestKubectlClientNodeSetWorkerPods(t *testing.T) {
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00pods\x00-n\x00test-cluster\x00-l\x00slurm.nebius.ai/nodeset=worker-gpu\x00-o\x00json": `{
			"items": [
				{
					"metadata": {"name": "worker-gpu-0", "uid": "first-pod"},
					"spec": {"hostname": "worker-gpu-0", "nodeName": "node-a"},
					"status": {"conditions": [{"type": "Ready", "status": "True"}]}
				},
				{
					"metadata": {"name": "worker-gpu-1", "uid": "second-pod"},
					"spec": {"hostname": "worker-gpu-1"},
					"status": {"phase": "Pending"}
				}
			]
		}`,
	}}

	pods, err := NewKubectlClient(exec).NodeSetWorkerPods(t.Context(), "test-cluster", "worker-gpu")
	require.NoError(t, err)
	require.Len(t, pods, 2)
	assert.Equal(t, "worker-gpu-0", pods[0].Name)
	assert.Equal(t, "node-a", pods[0].Spec.NodeName)
	assert.Equal(t, "first-pod", string(pods[0].UID))
	assert.Equal(t, "worker-gpu-1", pods[1].Name)
	assert.Equal(t, "Pending", string(pods[1].Status.Phase))
}

func TestKubectlClientWorkerPods(t *testing.T) {
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00pods\x00-n\x00soperator\x00-l\x00slurm.nebius.ai/worker=true\x00-o\x00json": `{
			"items": [
				{
					"metadata": {"name": "kube-worker-a", "labels": {"slurm.nebius.ai/nodeset": "worker-a-set"}},
					"spec": {"hostname": "worker-a", "nodeName": "node-a", "volumes": [{"name": "local-nvme", "emptyDir": {}}]},
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
		{SlurmNodeName: "worker-a", PodName: "kube-worker-a", NodeSetName: "worker-a-set", KubernetesNodeName: "node-a", Ready: true, HasLocalNVMe: true},
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

func TestKubectlClientWorkerContainerEnvironmentVariable(t *testing.T) {
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00pod\x00worker-0\x00-n\x00soperator\x00-o\x00json": `{
			"metadata": {"name": "worker-0"},
			"spec": {
				"containers": [
					{"name": "slurmd", "env": [
						{"name": "NVIDIA_DRIVER_CAPABILITIES", "value": "compute,graphics,utility,video"}
					]}
				]
			}
		}`,
	}}

	value, err := NewKubectlClient(exec).WorkerContainerEnvironmentVariable(
		t.Context(),
		WorkerPodInfo{PodName: "worker-0"},
		"slurmd",
		"NVIDIA_DRIVER_CAPABILITIES",
	)
	require.NoError(t, err)
	assert.Equal(t, "compute,graphics,utility,video", value)
}

func TestKubectlClientWorkerContainerEnvironmentVariableRejectsValueFrom(t *testing.T) {
	exec := &kubectlClientTestExec{kubectl: map[string]string{
		"get\x00pod\x00worker-0\x00-n\x00soperator\x00-o\x00json": `{
			"metadata": {"name": "worker-0"},
			"spec": {
				"containers": [
					{"name": "slurmd", "env": [
						{"name": "NVIDIA_DRIVER_CAPABILITIES", "valueFrom": {"secretKeyRef": {"name": "gpu", "key": "capabilities"}}}
					]}
				]
			}
		}`,
	}}

	_, err := NewKubectlClient(exec).WorkerContainerEnvironmentVariable(
		t.Context(),
		WorkerPodInfo{PodName: "worker-0"},
		"slurmd",
		"NVIDIA_DRIVER_CAPABILITIES",
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "valueFrom is not supported")
}

type kubectlClientTestExec struct {
	kubectl       map[string]string
	kubectlErrors map[string]error
	kubectlCalls  int
}

func (e *kubectlClientTestExec) Kubectl() ArgsScope {
	return NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		e.kubectlCalls++
		key := strings.Join(args, "\x00")
		if err := e.kubectlErrors[key]; err != nil {
			return "", err
		}
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
