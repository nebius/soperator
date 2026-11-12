package sharedsteps

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

func staticPowerDownNodeSet(name string, ephemeral *bool, replicas int32) kubeobjects.NodeSet {
	return kubeobjects.NodeSet{
		Metadata: kubeobjects.ObjectMeta{Name: name, Namespace: framework.SoperatorNamespace},
		Spec:     kubeobjects.NodeSetSpec{Replicas: replicas, EphemeralNodes: ephemeral},
	}
}

func staticPowerDownWorker(name, nodeSet, state string, hasGPU bool) framework.WorkerInfo {
	return framework.WorkerInfo{
		Name:        name,
		NodeSetName: nodeSet,
		HasGPU:      hasGPU,
		SlurmNode:   framework.SlurmNodeInfo{Name: name, State: state},
	}
}

func TestPickIdleStaticWorker(t *testing.T) {
	ephemeral, static := true, false
	nodeSets := []kubeobjects.NodeSet{
		staticPowerDownNodeSet("gpu", nil, 2),
		staticPowerDownNodeSet("cpu", &static, 2),
		staticPowerDownNodeSet("eph", &ephemeral, 2),
		staticPowerDownNodeSet("empty", &static, 0),
	}

	t.Run("prefers an idle CPU worker of a static NodeSet", func(t *testing.T) {
		worker, err := pickIdleStaticWorker(nodeSets, []framework.WorkerInfo{
			staticPowerDownWorker("eph-0", "eph", "IDLE+CLOUD", false),
			staticPowerDownWorker("gpu-0", "gpu", "IDLE+CLOUD", true),
			staticPowerDownWorker("cpu-1", "cpu", "IDLE+CLOUD", false),
			staticPowerDownWorker("cpu-0", "cpu", "ALLOCATED+CLOUD", false),
		})
		require.NoError(t, err)
		assert.Equal(t, "cpu-1", worker.Name)
	})

	t.Run("falls back to an idle GPU worker", func(t *testing.T) {
		worker, err := pickIdleStaticWorker(nodeSets, []framework.WorkerInfo{
			staticPowerDownWorker("gpu-1", "gpu", "IDLE+CLOUD", true),
			staticPowerDownWorker("cpu-0", "cpu", "MIXED+CLOUD", false),
		})
		require.NoError(t, err)
		assert.Equal(t, "gpu-1", worker.Name)
	})

	t.Run("skips a cluster without static workers", func(t *testing.T) {
		_, err := pickIdleStaticWorker(nodeSets, []framework.WorkerInfo{
			staticPowerDownWorker("eph-0", "eph", "IDLE+CLOUD", false),
		})
		assert.True(t, framework.IsInsufficientWorkers(err))
	})

	t.Run("fails when static workers are busy or unhealthy", func(t *testing.T) {
		_, err := pickIdleStaticWorker(nodeSets, []framework.WorkerInfo{
			staticPowerDownWorker("cpu-0", "cpu", "IDLE+CLOUD+POWERED_DOWN", false),
			staticPowerDownWorker("cpu-1", "cpu", "IDLE+DRAIN+CLOUD", false),
			staticPowerDownWorker("gpu-0", "gpu", "IDLE+COMPLETING+CLOUD", true),
		})
		require.Error(t, err)
		assert.False(t, framework.IsInsufficientWorkers(err))
		assert.ErrorContains(t, err, "cpu-0 state=IDLE+CLOUD+POWERED_DOWN")
		assert.ErrorContains(t, err, "cpu-1 state=IDLE+DRAIN+CLOUD")
		assert.ErrorContains(t, err, "gpu-0 state=IDLE+COMPLETING+CLOUD")
	})
}

func TestStaticPowerDownStates(t *testing.T) {
	for _, tt := range []struct {
		state            string
		poweringOrDown   bool
		powerDownPending bool
	}{
		{state: "IDLE+CLOUD"},
		{state: "IDLE+CLOUD+POWER_DOWN", powerDownPending: true},
		{state: "IDLE+CLOUD+POWERING_DOWN", poweringOrDown: true, powerDownPending: true},
		{state: "IDLE+CLOUD+POWERED_DOWN", poweringOrDown: true},
		{state: "DOWN+CLOUD+POWERED_DOWN", poweringOrDown: true},
	} {
		node := framework.SlurmNodeInfo{State: tt.state}
		assert.Equal(t, tt.poweringOrDown, isPoweringOrPoweredDown(node), "isPoweringOrPoweredDown(%s)", tt.state)
		assert.Equal(t, tt.powerDownPending, isPowerDownInProgress(node), "isPowerDownInProgress(%s)", tt.state)
	}
}

func TestCheckStaticWorkerReturned(t *testing.T) {
	started := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	target := staticPowerDownTarget{
		worker:  framework.WorkerInfo{SlurmNode: framework.SlurmNodeInfo{SlurmdStartTime: started}},
		podName: "cpu-0",
		podUID:  "old-uid",
	}
	readyPod := func(uid string) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "cpu-0", UID: types.UID(uid)},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			}},
		}
	}
	readyNode := framework.SlurmNodeInfo{Name: "cpu-0", State: "IDLE+CLOUD", SlurmdStartTime: started.Add(2 * time.Minute)}

	for _, tt := range []struct {
		name    string
		pod     corev1.Pod
		found   bool
		node    framework.SlurmNodeInfo
		wantErr string
	}{
		{name: "returned", pod: readyPod("new-uid"), found: true, node: readyNode},
		{name: "pod is absent", node: readyNode, wantErr: "worker pod cpu-0 is absent"},
		{name: "pod is not recreated", pod: readyPod("old-uid"), found: true, node: readyNode, wantErr: "still has the original UID old-uid"},
		{
			name: "recreated pod is not ready", found: true, node: readyNode,
			pod:     corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "cpu-0", UID: types.UID("new-uid")}},
			wantErr: "recreated worker pod cpu-0 (UID new-uid) is not ready",
		},
		{
			name: "node is still powered down", pod: readyPod("new-uid"), found: true,
			node:    framework.SlurmNodeInfo{Name: "cpu-0", State: "IDLE+CLOUD+POWERED_DOWN", SlurmdStartTime: started.Add(time.Minute)},
			wantErr: "state=IDLE+CLOUD+POWERED_DOWN",
		},
		{
			name: "slurmd did not restart", pod: readyPod("new-uid"), found: true,
			node:    framework.SlurmNodeInfo{Name: "cpu-0", State: "IDLE+CLOUD", SlurmdStartTime: started},
			wantErr: "expected later than 2026-11-16T10:00:00Z",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := checkStaticWorkerReturned(target, tt.pod, tt.found, tt.node)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestCRDSpecHasField(t *testing.T) {
	for _, tt := range []struct {
		name string
		crd  string
		want bool
	}{
		{
			name: "served version declares the field",
			crd:  `{"spec":{"versions":[{"name":"v1alpha1","served":true,"schema":{"openAPIV3Schema":{"properties":{"spec":{"properties":{"activeNodes":{},"recreateNodes":{"type":"array"}}}}}}}]}}`,
			want: true,
		},
		{
			name: "served version lacks the field",
			crd:  `{"spec":{"versions":[{"name":"v1alpha1","served":true,"schema":{"openAPIV3Schema":{"properties":{"spec":{"properties":{"activeNodes":{}}}}}}}]}}`,
		},
		{
			name: "only an unserved version declares the field",
			crd:  `{"spec":{"versions":[{"name":"v1alpha1","served":false,"schema":{"openAPIV3Schema":{"properties":{"spec":{"properties":{"recreateNodes":{}}}}}}}]}}`,
		},
		{name: "schema without spec", crd: `{"spec":{"versions":[{"name":"v1alpha1","served":true}]}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var crd customResourceDefinition
			require.NoError(t, json.Unmarshal([]byte(tt.crd), &crd))
			assert.Equal(t, tt.want, crdSpecHasField(crd, "recreateNodes"))
		})
	}
}

func TestStaticPowerDownCommandQuotesWorker(t *testing.T) {
	assert.Equal(t, "scontrol power down 'cpu-0'", staticPowerDownCommand("cpu-0"))
}
