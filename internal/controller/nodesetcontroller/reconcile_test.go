package nodesetcontroller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
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
