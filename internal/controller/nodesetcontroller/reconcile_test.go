package nodesetcontroller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
)

func makeNodeSetWithConditions(conditions ...metav1.Condition) *slurmv1alpha1.NodeSet {
	return &slurmv1alpha1.NodeSet{
		Status: slurmv1alpha1.NodeSetStatus{
			Conditions: conditions,
		},
	}
}

func TestComputePhase(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		want       string
	}{
		{
			name:       "no conditions — Pending",
			conditions: nil,
			want:       slurmv1alpha1.PhaseNodeSetPending,
		},
		{
			name: "all conditions Unknown — Pending",
			conditions: []metav1.Condition{
				{Type: slurmv1alpha1.ConditionNodeSetStatefulSetTerminated, Status: metav1.ConditionUnknown},
				{Type: slurmv1alpha1.ConditionNodeSetPodsReady, Status: metav1.ConditionUnknown},
			},
			want: slurmv1alpha1.PhaseNodeSetPending,
		},
		{
			name: "StatefulSetTerminated=True — Terminating",
			conditions: []metav1.Condition{
				{Type: slurmv1alpha1.ConditionNodeSetStatefulSetTerminated, Status: metav1.ConditionTrue, Reason: "Maintenance"},
			},
			want: slurmv1alpha1.PhaseNodeSetTerminating,
		},
		{
			name: "StatefulSetTerminated=True takes priority over PodsReady=True",
			conditions: []metav1.Condition{
				{Type: slurmv1alpha1.ConditionNodeSetStatefulSetTerminated, Status: metav1.ConditionTrue, Reason: "Maintenance"},
				{Type: slurmv1alpha1.ConditionNodeSetPodsReady, Status: metav1.ConditionTrue, Reason: "NodeSetReady"},
			},
			want: slurmv1alpha1.PhaseNodeSetTerminating,
		},
		{
			name: "PodsReady=True, not terminated — Ready",
			conditions: []metav1.Condition{
				{Type: slurmv1alpha1.ConditionNodeSetStatefulSetTerminated, Status: metav1.ConditionFalse, Reason: "WorkersEnabled"},
				{Type: slurmv1alpha1.ConditionNodeSetPodsReady, Status: metav1.ConditionTrue, Reason: "NodeSetReady"},
			},
			want: slurmv1alpha1.PhaseNodeSetReady,
		},
		{
			name: "PodsReady=False, not terminated — Provisioning",
			conditions: []metav1.Condition{
				{Type: slurmv1alpha1.ConditionNodeSetStatefulSetTerminated, Status: metav1.ConditionFalse, Reason: "WorkersEnabled"},
				{Type: slurmv1alpha1.ConditionNodeSetPodsReady, Status: metav1.ConditionFalse, Reason: "NodeSetNotReady"},
			},
			want: slurmv1alpha1.PhaseNodeSetProvisioning,
		},
		{
			name: "PodsReady=Unknown, not terminated — Pending",
			conditions: []metav1.Condition{
				{Type: slurmv1alpha1.ConditionNodeSetStatefulSetTerminated, Status: metav1.ConditionFalse, Reason: "WorkersEnabled"},
				{Type: slurmv1alpha1.ConditionNodeSetPodsReady, Status: metav1.ConditionUnknown, Reason: "SetUpCondition"},
			},
			want: slurmv1alpha1.PhaseNodeSetPending,
		},
		{
			name: "only PodsReady=True, no terminated condition — Ready",
			conditions: []metav1.Condition{
				{Type: slurmv1alpha1.ConditionNodeSetPodsReady, Status: metav1.ConditionTrue, Reason: "NodeSetReady"},
			},
			want: slurmv1alpha1.PhaseNodeSetReady,
		},
		{
			name: "only PodsReady=False, no terminated condition — Provisioning",
			conditions: []metav1.Condition{
				{Type: slurmv1alpha1.ConditionNodeSetPodsReady, Status: metav1.ConditionFalse, Reason: "NodeSetNotReady"},
			},
			want: slurmv1alpha1.PhaseNodeSetProvisioning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeSet := makeNodeSetWithConditions(tt.conditions...)
			got := computePhase(nodeSet)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReconcileNodeSetPowerState_AllowsZeroInitialEphemeralNodes(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))

	nodeSet := &slurmv1alpha1.NodeSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-nodeset",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
		Spec: slurmv1alpha1.NodeSetSpec{
			InitialNumberEphemeralNodes: 0,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nodeSet).
		WithStatusSubresource(&slurmv1alpha1.NodeSetPowerState{}).
		Build()

	r := &NodeSetReconciler{
		Reconciler: reconciler.NewReconciler(fakeClient, scheme, record.NewFakeRecorder(10)),
		NodeSetPowerState: reconciler.NewNodeSetPowerStateReconciler(
			reconciler.NewReconciler(fakeClient, scheme, record.NewFakeRecorder(10)),
		),
	}

	activeNodes, err := r.reconcileNodeSetPowerState(context.Background(), nodeSet)
	require.NoError(t, err)
	assert.Empty(t, activeNodes)

	var powerState slurmv1alpha1.NodeSetPowerState
	err = fakeClient.Get(context.Background(), client.ObjectKey{
		Namespace: nodeSet.Namespace,
		Name:      nodeSet.Name,
	}, &powerState)
	require.NoError(t, err)

	assert.Equal(t, nodeSet.Name, powerState.Spec.NodeSetRef)
	assert.Empty(t, powerState.Spec.ActiveNodes)
	assert.Equal(t, int32(0), powerState.Status.ActiveCount)
	readyCondition := meta.FindStatusCondition(powerState.Status.Conditions, slurmv1alpha1.ConditionNodeSetPowerStateReady)
	require.NotNil(t, readyCondition)
	assert.Equal(t, metav1.ConditionTrue, readyCondition.Status)
}
