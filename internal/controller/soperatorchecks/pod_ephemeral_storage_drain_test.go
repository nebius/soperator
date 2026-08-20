package soperatorchecks

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

// The check drains above 85% and resumes below 80%; the gap between them is what keeps a
// node from flapping.
const (
	drainTestUsageThreshold  = 85.0
	drainTestResumeThreshold = 80.0

	drainTestLimitBytes = 1 << 30 // 1Gi, matching the pod's ephemeral-storage limit below
	drainTestPodName    = "worker-0"
	drainTestNamespace  = "soperator"
	drainTestNodeName   = "test-node"
	drainTestPodUID     = "uid-worker-0"
)

func slurmNodeStates(states ...api.V0044NodeState) map[api.V0044NodeState]struct{} {
	out := make(map[api.V0044NodeState]struct{}, len(states))
	for _, s := range states {
		out[s] = struct{}{}
	}
	return out
}

// drainTestEnv wires a fake kubelet reporting a chosen usage level to a controller backed by
// a mocked Slurm API, so a whole reconcile can be driven end to end.
type drainTestEnv struct {
	controller  *PodEphemeralStorageCheck
	slurmMock   *slurmapifake.MockClient
	pod         *corev1.Pod
	kubeletHits *atomic.Int32
	clusterKey  types.NamespacedName
}

func newDrainTestEnv(t *testing.T, usagePercent float64, slurmNode slurmapi.Node, mutate ...func(*corev1.Pod)) *drainTestEnv {
	t.Helper()

	usedBytes := uint64(float64(drainTestLimitBytes) * usagePercent / 100.0)

	var hits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != kubeletSummaryPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"node":{"nodeName":%q},"pods":[{"podRef":{"name":%q,"namespace":%q,"uid":%q},"ephemeral-storage":{"usedBytes":%d}}]}`,
			drainTestNodeName, drainTestPodName, drainTestNamespace, drainTestPodUID, usedBytes)))
	}))
	t.Cleanup(server.Close)

	host, port := splitHostPort(t, server.URL)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      drainTestPodName,
			Namespace: drainTestNamespace,
			UID:       types.UID(drainTestPodUID),
			Labels: map[string]string{
				consts.LabelWorkerKey:    consts.LabelWorkerValue,
				consts.LabelManagedByKey: consts.LabelManagedByValue,
			},
			OwnerReferences: []metav1.OwnerReference{{
				Kind:       "StatefulSet",
				APIVersion: "apps.kruise.io/v1beta1",
				Name:       "worker",
			}},
		},
		Spec: corev1.PodSpec{
			NodeName: drainTestNodeName,
			Containers: []corev1.Container{{
				Name: "slurmd",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	for _, m := range mutate {
		m(pod)
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: drainTestNodeName},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: host}},
			DaemonEndpoints: corev1.NodeDaemonEndpoints{
				KubeletEndpoint: corev1.DaemonEndpoint{Port: port},
			},
		},
	}

	cluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{Name: drainTestNamespace, Namespace: drainTestNamespace},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))

	fakeClient := clientfake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pod, node, cluster).
		Build()

	clusterKey := types.NamespacedName{Name: drainTestNamespace, Namespace: drainTestNamespace}

	slurmMock := slurmapifake.NewMockClient(t)
	slurmMock.EXPECT().ListNodes(mock.Anything).Return([]slurmapi.Node{slurmNode}, nil).Maybe()

	clientSet := slurmapi.NewClientSet(t.Context())
	clientSet.AddClient(clusterKey, slurmMock)

	controller, err := NewPodEphemeralStorageCheck(
		fakeClient,
		scheme,
		record.NewFakeRecorder(100),
		&rest.Config{},
		time.Minute,
		drainTestUsageThreshold,
		drainTestResumeThreshold,
		clientSet,
		KubeletClientConfig{InsecureSkipTLSVerify: true},
	)
	require.NoError(t, err)

	return &drainTestEnv{
		controller:  controller,
		slurmMock:   slurmMock,
		pod:         pod,
		kubeletHits: &hits,
		clusterKey:  clusterKey,
	}
}

// expectNodeUpdate records the single state update the check is expected to post and
// captures the message that goes with it.
func expectNodeUpdate(m *slurmapifake.MockClient, state api.V0044UpdateNodeMsgState, captured *string) {
	m.EXPECT().
		SlurmV0044PostNodeWithResponse(mock.Anything, drainTestPodName, mock.MatchedBy(func(body api.V0044UpdateNodeMsg) bool {
			return body.State != nil && len(*body.State) == 1 && (*body.State)[0] == state
		})).
		Run(func(_ context.Context, _ string, body api.V0044UpdateNodeMsg, _ ...api.RequestEditorFn) {
			if body.Reason != nil {
				*captured = *body.Reason
			}
		}).
		Return(&api.SlurmV0044PostNodeResponse{JSON200: &api.V0044OpenapiResp{}}, nil).
		Once()
}

// Usage above the threshold on a healthy node must drain it, with a reason an operator can
// act on: the [user_problem] prefix is what the undrain path later keys off.
func TestEphemeralStorageDrainsNodeAboveThreshold(t *testing.T) {
	var reason string
	env := newDrainTestEnv(t, 87.0, slurmapi.Node{
		Name:   drainTestPodName,
		States: slurmNodeStates(api.V0044NodeStateIDLE),
	})
	expectNodeUpdate(env.slurmMock, api.V0044UpdateNodeMsgStateDRAIN, &reason)

	require.NoError(t, env.controller.ReconcilePodEphemeralStorageCheckForPod(t.Context(), env.pod))

	assert.Contains(t, reason, consts.SlurmUserReasonHC+" pod_ephemeral_storage")
	assert.Contains(t, reason, "87.00%")
}

// A node already parked by a previous run must not be drained again on every reconcile.
func TestEphemeralStorageDoesNotRedrainDrainedNode(t *testing.T) {
	env := newDrainTestEnv(t, 87.0, slurmapi.Node{
		Name:   drainTestPodName,
		States: slurmNodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN),
		Reason: &slurmapi.NodeReason{Reason: consts.SlurmUserReasonHC + " pod_ephemeral_storage 90.00% of ephemeral storage is used."},
	})

	require.NoError(t, env.controller.ReconcilePodEphemeralStorageCheckForPod(t.Context(), env.pod))

	env.slurmMock.AssertNotCalled(t, "SlurmV0044PostNodeWithResponse", mock.Anything, mock.Anything, mock.Anything)
}

// Once the disk is freed the node this check parked must come back automatically.
func TestEphemeralStorageUndrainsAfterCleanup(t *testing.T) {
	var reason string
	env := newDrainTestEnv(t, 50.0, slurmapi.Node{
		Name:   drainTestPodName,
		States: slurmNodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN),
		Reason: &slurmapi.NodeReason{Reason: consts.SlurmUserReasonHC + " pod_ephemeral_storage 87.00% of ephemeral storage is used."},
	})
	expectNodeUpdate(env.slurmMock, api.V0044UpdateNodeMsgStateRESUME, &reason)

	require.NoError(t, env.controller.ReconcilePodEphemeralStorageCheckForPod(t.Context(), env.pod))
}

// A node drained by someone else must stay drained, however much disk is free.
func TestEphemeralStorageLeavesForeignDrainAlone(t *testing.T) {
	env := newDrainTestEnv(t, 50.0, slurmapi.Node{
		Name:   drainTestPodName,
		States: slurmNodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN),
		Reason: &slurmapi.NodeReason{Reason: "admin maintenance"},
	})

	require.NoError(t, env.controller.ReconcilePodEphemeralStorageCheckForPod(t.Context(), env.pod))

	env.slurmMock.AssertNotCalled(t, "SlurmV0044PostNodeWithResponse", mock.Anything, mock.Anything, mock.Anything)
}

// Between the two thresholds nothing happens, which is what stops drain/undrain flapping.
func TestEphemeralStorageIgnoresUsageBetweenThresholds(t *testing.T) {
	env := newDrainTestEnv(t, 82.0, slurmapi.Node{
		Name:   drainTestPodName,
		States: slurmNodeStates(api.V0044NodeStateIDLE),
	})

	require.NoError(t, env.controller.ReconcilePodEphemeralStorageCheckForPod(t.Context(), env.pod))

	env.slurmMock.AssertNotCalled(t, "SlurmV0044PostNodeWithResponse", mock.Anything, mock.Anything, mock.Anything)
}

// A pod without an ephemeral-storage limit has nothing to compare usage against.
func TestEphemeralStorageSkipsPodWithoutLimit(t *testing.T) {
	env := newDrainTestEnv(t, 99.0, slurmapi.Node{
		Name:   drainTestPodName,
		States: slurmNodeStates(api.V0044NodeStateIDLE),
	}, func(p *corev1.Pod) {
		p.Spec.Containers[0].Resources.Limits = nil
	})

	require.NoError(t, env.controller.ReconcilePodEphemeralStorageCheckForPod(t.Context(), env.pod))

	env.slurmMock.AssertNotCalled(t, "SlurmV0044PostNodeWithResponse", mock.Anything, mock.Anything, mock.Anything)
}

// Requirement: only nodes that host worker pods may be queried. A pod outside the
// NodeSet -> ASTS -> Pod scheme must not cause a kubelet request at all.
func TestEphemeralStorageReconcileSkipsNonWorkerPod(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*corev1.Pod)
	}{
		{"missing worker label", func(p *corev1.Pod) { delete(p.Labels, consts.LabelWorkerKey) }},
		{"not managed by soperator", func(p *corev1.Pod) { p.Labels[consts.LabelManagedByKey] = "someone-else" }},
		{"not owned by a kruise StatefulSet", func(p *corev1.Pod) {
			p.OwnerReferences = []metav1.OwnerReference{{Kind: "DaemonSet", APIVersion: "apps/v1", Name: "ds"}}
		}},
		{"no owner at all", func(p *corev1.Pod) { p.OwnerReferences = nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newDrainTestEnv(t, 99.0, slurmapi.Node{
				Name:   drainTestPodName,
				States: slurmNodeStates(api.V0044NodeStateIDLE),
			}, tt.mutate)

			res, err := env.controller.Reconcile(t.Context(), ctrlRequestFor(env.pod))
			require.NoError(t, err)

			assert.Zero(t, res.RequeueAfter, "an irrelevant pod must not be requeued")
			assert.Zero(t, env.kubeletHits.Load(), "kubelet must not be queried for a non-worker pod")
			env.slurmMock.AssertNotCalled(t, "SlurmV0044PostNodeWithResponse", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

// A worker pod, by contrast, is reconciled and does reach the kubelet.
func TestEphemeralStorageReconcileQueriesWorkerPod(t *testing.T) {
	var reason string
	env := newDrainTestEnv(t, 87.0, slurmapi.Node{
		Name:   drainTestPodName,
		States: slurmNodeStates(api.V0044NodeStateIDLE),
	})
	expectNodeUpdate(env.slurmMock, api.V0044UpdateNodeMsgStateDRAIN, &reason)

	res, err := env.controller.Reconcile(t.Context(), ctrlRequestFor(env.pod))
	require.NoError(t, err)

	assert.Equal(t, time.Minute, res.RequeueAfter)
	assert.Equal(t, int32(1), env.kubeletHits.Load())
	assert.Contains(t, reason, consts.SlurmUserReasonHC+" pod_ephemeral_storage")
}

// The drain reason must carry actionable remediation, since operators read it from scontrol.
func TestEphemeralStorageDrainReasonIsActionable(t *testing.T) {
	var reason string
	env := newDrainTestEnv(t, 91.0, slurmapi.Node{
		Name:   drainTestPodName,
		States: slurmNodeStates(api.V0044NodeStateIDLE),
	})
	expectNodeUpdate(env.slurmMock, api.V0044UpdateNodeMsgStateDRAIN, &reason)

	require.NoError(t, env.controller.ReconcilePodEphemeralStorageCheckForPod(t.Context(), env.pod))

	for _, want := range []string{"fs_usage.sh", "enroot list", "docker ps -a", "scontrol reboot", "state=resume"} {
		assert.Contains(t, reason, want)
	}
}

// An event is recorded so the condition is visible from kubectl describe, not only in Slurm.
func TestEphemeralStorageRecordsWarningEvent(t *testing.T) {
	var reason string
	env := newDrainTestEnv(t, 87.0, slurmapi.Node{
		Name:   drainTestPodName,
		States: slurmNodeStates(api.V0044NodeStateIDLE),
	})
	expectNodeUpdate(env.slurmMock, api.V0044UpdateNodeMsgStateDRAIN, &reason)

	require.NoError(t, env.controller.ReconcilePodEphemeralStorageCheckForPod(t.Context(), env.pod))

	events := &corev1.EventList{}
	require.NoError(t, env.controller.List(t.Context(), events))
	require.Len(t, events.Items, 1)
	assert.Equal(t, consts.HighEphemeralStorageUsage, events.Items[0].Reason)
	assert.Equal(t, corev1.EventTypeWarning, events.Items[0].Type)
	assert.Equal(t, drainTestPodName, events.Items[0].InvolvedObject.Name)
}

func ctrlRequestFor(pod *corev1.Pod) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}}
}
