package topologyconfcontroller_test

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stretchr/testify/require"

	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
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
				"unknown": {"pod1", "pod2"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.GroupPodNamesByNode(tt.pods)
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

func TestCollectRunningWorkerPods(t *testing.T) {
	t.Parallel()

	const (
		namespace   = "test-ns"
		clusterName = "cluster-a"
	)

	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	nodeSetList := []v1alpha1.NodeSet{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nodeset-a",
				Namespace: namespace,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nodeset-b",
				Namespace: namespace,
			},
		},
	}

	objects := []client.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-running-a1",
				Namespace: namespace,
				Labels: map[string]string{
					consts.LabelNodeSetKey: "nodeset-a",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-pending-a",
				Namespace: namespace,
				Labels: map[string]string{
					consts.LabelNodeSetKey: "nodeset-a",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-running-b1",
				Namespace: namespace,
				Labels: map[string]string{
					consts.LabelNodeSetKey: "nodeset-b",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-running-other-cluster",
				Namespace: namespace,
				Labels: map[string]string{
					consts.LabelNodeSetKey: "nodeset-other-cluster",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-running-other-ns",
				Namespace: "other-ns",
				Labels: map[string]string{
					consts.LabelNodeSetKey: "nodeset-a",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithIndex(&corev1.Pod{}, consts.FieldStatusPhase, func(obj client.Object) []string {
			pod := obj.(*corev1.Pod)
			return []string{string(pod.Status.Phase)}
		}).
		Build()

	reconciler := tc.NewWorkerTopologyReconciler(fakeClient, scheme, namespace)

	pods, err := reconciler.CollectRunningWorkerPods(context.Background(), nodeSetList, clusterName, namespace)
	require.NoError(t, err)

	var names []string
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	slices.Sort(names)

	assert.Equal(t, []string{"pod-running-a1", "pod-running-b1"}, names)
}
