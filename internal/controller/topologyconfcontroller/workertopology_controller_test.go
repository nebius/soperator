package topologyconfcontroller_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"

	tc "nebius.ai/slurm-operator/internal/controller/topologyconfcontroller"
)

func TestGetPodByNode(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}

	tests := []struct {
		name     string
		pods     []corev1.Pod
		expected map[string][]string
	}{
		{
			name: "Pods with NodeName",
			pods: []corev1.Pod{
				{Spec: corev1.PodSpec{NodeName: "node1"}, ObjectMeta: metav1.ObjectMeta{Name: "pod1"}},
				{Spec: corev1.PodSpec{NodeName: "node2"}, ObjectMeta: metav1.ObjectMeta{Name: "pod2"}},
				{Spec: corev1.PodSpec{NodeName: "node1"}, ObjectMeta: metav1.ObjectMeta{Name: "pod3"}},
			},
			expected: map[string][]string{
				"node1": {"pod1", "pod3"},
				"node2": {"pod2"},
			},
		},
		{
			name: "Pods without NodeName",
			pods: []corev1.Pod{
				{Spec: corev1.PodSpec{NodeName: ""}, ObjectMeta: metav1.ObjectMeta{Name: "pod1"}},
				{Spec: corev1.PodSpec{NodeName: ""}, ObjectMeta: metav1.ObjectMeta{Name: "pod2"}},
			},
			expected: map[string][]string{
				"": {"pod1", "pod2"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.GetPodsByNode(tt.pods)
			require.Equal(t, tt.expected, result, "Test %s failed: expected %v, got %v", tt.name, tt.expected, result)
		})
	}
}

func TestParseNodeTopologyLabels(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}
	tests := []struct {
		name        string
		input       map[string]string
		expected    map[string]tc.NodeTopologyLabels
		expectError bool
	}{
		{
			name: "Valid topology data",
			input: map[string]string{
				"node1": `{"tier-1":"sw0","tier-2":"spine0"}`,
				"node2": `{"tier-1":"sw1","tier-2":"spine1","tier-3":"leaf0"}`,
			},
			expected: map[string]tc.NodeTopologyLabels{
				"node1": {"tier-1": "sw0", "tier-2": "spine0"},
				"node2": {"tier-1": "sw1", "tier-2": "spine1", "tier-3": "leaf0"},
			},
			expectError: false,
		},
		{
			name: "Invalid JSON data",
			input: map[string]string{
				"node1": `{"tier-1":"sw0","tier-2":"spine0"`, // Missing closing brace
			},
			expected:    nil,
			expectError: true,
		},
		{
			name:        "Empty input data",
			input:       map[string]string{},
			expected:    map[string]tc.NodeTopologyLabels{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := reconciler.ParseNodeTopologyLabels(tt.input)

			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				require.Nil(t, result, "Result should be nil when an error occurs")
			} else {
				require.NoError(t, err, "Unexpected error occurred")
				require.Equal(t, tt.expected, result, "Deserialized topology does not match expected result")
			}
		})
	}
}

func TestInitializeTopologyConf(t *testing.T) {
	tests := []struct {
		name         string
		statefulSets []kruisev1b1.StatefulSet
		expected     string
	}{
		{
			name:         "No StatefulSets",
			statefulSets: []kruisev1b1.StatefulSet{},
			expected:     "",
		},
		{
			name: "Single StatefulSet with replicas",
			statefulSets: []kruisev1b1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "worker-sts",
					},
					Spec: kruisev1b1.StatefulSetSpec{
						Replicas: ptr.To(int32(3)),
					},
				},
			},
			expected: "SwitchName=unknown Nodes=worker-sts0,worker-sts1,worker-sts2",
		},
		{
			name: "Multiple StatefulSets with replicas",
			statefulSets: []kruisev1b1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "worker-sts1",
					},
					Spec: kruisev1b1.StatefulSetSpec{
						Replicas: ptr.To(int32(2)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "worker-sts2",
					},
					Spec: kruisev1b1.StatefulSetSpec{
						Replicas: ptr.To(int32(1)),
					},
				},
			},
			expected: "SwitchName=unknown Nodes=worker-sts10,worker-sts11,worker-sts20",
		},
		{
			name: "StatefulSet with zero replicas",
			statefulSets: []kruisev1b1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "worker-sts",
					},
					Spec: kruisev1b1.StatefulSetSpec{
						Replicas: ptr.To(int32(0)),
					},
				},
			},
			expected: "SwitchName=unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aSTS := &kruisev1b1.StatefulSetList{
				Items: tt.statefulSets,
			}

			result := tc.InitializeTopologyConf(aSTS)
			assert.Equal(t, tt.expected, result)
		})
	}
}
