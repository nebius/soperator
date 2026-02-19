package topologyconfcontroller_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
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

// TestWorkerTopologyReconciler_GetStatefulSetsWithFallback tests the StatefulSet retrieval with fallback logic
func TestWorkerTopologyReconciler_GetStatefulSetsWithFallback(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, kruisev1b1.AddToScheme(scheme))

	ctx := context.Background()
	namespace := "test-namespace"
	clusterName := "test-cluster"

	tests := []struct {
		name             string
		slurmCluster     *slurmv1.SlurmCluster
		existingObjs     []client.Object
		expectedSTS      int
		expectedNames    []string
		expectedReplicas []int32
	}{
		{
			name: "NodeSets exist and worker.Size == 0 - builds from NodeSets",
			slurmCluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: slurmv1.SlurmClusterSpec{
					SlurmNodes: slurmv1.SlurmNodes{
						Worker: &slurmv1.SlurmNodeWorker{
							SlurmNode: slurmv1.SlurmNode{
								Size: 0,
							},
						},
					},
				},
			},
			existingObjs: []client.Object{
				&v1alpha1.NodeSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-workers",
						Namespace: namespace,
						Annotations: map[string]string{
							consts.AnnotationParentalClusterRefName: clusterName,
						},
					},
					Spec: v1alpha1.NodeSetSpec{
						Replicas: 3,
					},
				},
				&v1alpha1.NodeSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cpu-workers",
						Namespace: namespace,
						Annotations: map[string]string{
							consts.AnnotationParentalClusterRefName: clusterName,
						},
					},
					Spec: v1alpha1.NodeSetSpec{
						Replicas: 5,
					},
				},
			},
			expectedSTS:      2,
			expectedNames:    []string{"cpu-workers", "gpu-workers"},
			expectedReplicas: []int32{5, 3},
		},
		{
			name: "NodeSets exist but worker.Size > 0 - fallback to worker.Size",
			slurmCluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: slurmv1.SlurmClusterSpec{
					SlurmNodes: slurmv1.SlurmNodes{
						Worker: &slurmv1.SlurmNodeWorker{
							SlurmNode: slurmv1.SlurmNode{
								Size: 10,
							},
						},
					},
				},
			},
			existingObjs: []client.Object{
				&v1alpha1.NodeSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-workers",
						Namespace: namespace,
						Annotations: map[string]string{
							consts.AnnotationParentalClusterRefName: clusterName,
						},
					},
					Spec: v1alpha1.NodeSetSpec{
						Replicas: 3,
					},
				},
			},
			expectedSTS:      1,
			expectedNames:    []string{"worker"},
			expectedReplicas: []int32{10},
		},
		{
			name: "no NodeSets and worker.Size > 0 - fallback to worker.Size",
			slurmCluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: slurmv1.SlurmClusterSpec{
					SlurmNodes: slurmv1.SlurmNodes{
						Worker: &slurmv1.SlurmNodeWorker{
							SlurmNode: slurmv1.SlurmNode{
								Size: 5,
							},
						},
					},
				},
			},
			existingObjs:     []client.Object{},
			expectedSTS:      1,
			expectedNames:    []string{"worker"},
			expectedReplicas: []int32{5},
		},
		{
			name: "no NodeSets and worker is nil - fallback with 0 replicas",
			slurmCluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: slurmv1.SlurmClusterSpec{},
			},
			existingObjs:     []client.Object{},
			expectedSTS:      1,
			expectedNames:    []string{"worker"},
			expectedReplicas: []int32{0},
		},
		{
			name: "NodeSets from different cluster are ignored",
			slurmCluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: slurmv1.SlurmClusterSpec{
					SlurmNodes: slurmv1.SlurmNodes{
						Worker: &slurmv1.SlurmNodeWorker{
							SlurmNode: slurmv1.SlurmNode{
								Size: 0,
							},
						},
					},
				},
			},
			existingObjs: []client.Object{
				&v1alpha1.NodeSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-workers",
						Namespace: namespace,
						Annotations: map[string]string{
							consts.AnnotationParentalClusterRefName: "other-cluster",
						},
					},
					Spec: v1alpha1.NodeSetSpec{
						Replicas: 3,
					},
				},
			},
			expectedSTS:      1,
			expectedNames:    []string{"worker"},
			expectedReplicas: []int32{0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjs...).
				Build()

			reconciler := &tc.WorkerTopologyReconciler{
				BaseReconciler: tc.BaseReconciler{
					Client: fakeClient,
					Scheme: scheme,
				},
			}

			result, err := reconciler.GetStatefulSetsWithFallback(ctx, namespace, tt.slurmCluster)

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Len(t, result.Items, tt.expectedSTS)

			for i, item := range result.Items {
				assert.Equal(t, tt.expectedNames[i], item.Name)
				require.NotNil(t, item.Spec.Replicas)
				assert.Equal(t, tt.expectedReplicas[i], *item.Spec.Replicas)
			}
		})
	}
}
