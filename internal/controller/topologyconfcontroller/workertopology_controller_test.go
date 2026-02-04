package topologyconfcontroller_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	tc "nebius.ai/slurm-operator/internal/controller/topologyconfcontroller"
	"nebius.ai/slurm-operator/internal/render/common"
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
	t.Run("Nil StatefulSetList", func(t *testing.T) {
		result := tc.InitializeTopologyConf(nil)
		assert.Equal(t, "SwitchName=root", result)
	})

	tests := []struct {
		name         string
		statefulSets []kruisev1b1.StatefulSet
		expected     string
	}{
		{
			name:         "No StatefulSets",
			statefulSets: []kruisev1b1.StatefulSet{},
			expected:     "SwitchName=unknown",
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
			expected: "SwitchName=unknown Nodes=worker-sts-0,worker-sts-1,worker-sts-2",
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
			expected: "SwitchName=unknown Nodes=worker-sts1-0,worker-sts1-1,worker-sts2-0",
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

func TestWorkerTopologyReconciler_IsNodeSets(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, kruisev1b1.AddToScheme(scheme))

	ctx := context.Background()
	namespace := "test-namespace"

	tests := []struct {
		name         string
		existingObjs []client.Object
		cluster      *slurmv1.SlurmCluster
		expected     bool
	}{
		{
			name: "NodeSets exist in namespace - returns true",
			existingObjs: []client.Object{
				&v1alpha1.NodeSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nodeset-1",
						Namespace: namespace,
					},
				},
			},
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: namespace,
				},
			},
			expected: true,
		},
		{
			name:         "No NodeSets and worker size > 0 - returns false",
			existingObjs: []client.Object{},
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
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
			expected: false,
		},
		{
			name:         "No NodeSets and worker size == 0 - returns true",
			existingObjs: []client.Object{},
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
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
			expected: true,
		},
		{
			name:         "No NodeSets and nil worker - returns true",
			existingObjs: []client.Object{},
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: namespace,
				},
				Spec: slurmv1.SlurmClusterSpec{
					SlurmNodes: slurmv1.SlurmNodes{
						Worker: nil,
					},
				},
			},
			expected: true,
		},
		{
			name: "Multiple NodeSets exist - returns true",
			existingObjs: []client.Object{
				&v1alpha1.NodeSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nodeset-1",
						Namespace: namespace,
					},
				},
				&v1alpha1.NodeSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nodeset-2",
						Namespace: namespace,
					},
				},
			},
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
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
			expected: true,
		},
		{
			name: "NodeSets in different namespace - returns false",
			existingObjs: []client.Object{
				&v1alpha1.NodeSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nodeset-1",
						Namespace: "other-namespace",
					},
				},
			},
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
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
			expected: false,
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

			result, err := reconciler.IsNodeSets(ctx, tt.cluster)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result, "IsNodeSets result mismatch")
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
		existingObjs     []client.Object
		expectError      bool
		errorContains    string
		expectedSTS      int
		expectedName     string
		expectedReplicas int32
	}{
		{
			name: "existing StatefulSets found - no fallback needed",
			existingObjs: []client.Object{
				&kruisev1b1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "worker-group-1",
						Namespace: namespace,
						Labels: labels.Merge(
							common.RenderLabels(consts.ComponentTypeWorker, clusterName),
							labels.Set{consts.LabelWorkerKey: consts.LabelWorkerValue},
						),
					},
					Spec: kruisev1b1.StatefulSetSpec{
						Replicas: ptr.To(int32(3)),
					},
				},
			},
			expectError:      false,
			expectedSTS:      1,
			expectedName:     "worker-group-1",
			expectedReplicas: 3,
		},
		{
			name: "no StatefulSets found but SlurmCluster exists - creates fallback",
			existingObjs: []client.Object{
				&slurmv1.SlurmCluster{
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
			},
			expectError:      false,
			expectedSTS:      1,
			expectedName:     "worker",
			expectedReplicas: 5,
		},
		{
			name:          "no StatefulSets and no SlurmCluster - returns error",
			existingObjs:  []client.Object{},
			expectError:   true,
			errorContains: "get SlurmCluster for fallback topology",
		},
		{
			name: "multiple StatefulSets found - returns all",
			existingObjs: []client.Object{
				&kruisev1b1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "worker-group-1",
						Namespace: namespace,
						Labels: labels.Merge(
							common.RenderLabels(consts.ComponentTypeWorker, clusterName),
							labels.Set{consts.LabelWorkerKey: consts.LabelWorkerValue},
						),
					},
					Spec: kruisev1b1.StatefulSetSpec{
						Replicas: ptr.To(int32(2)),
					},
				},
				&kruisev1b1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "worker-group-2",
						Namespace: namespace,
						Labels: labels.Merge(
							common.RenderLabels(consts.ComponentTypeWorker, clusterName),
							labels.Set{consts.LabelWorkerKey: consts.LabelWorkerValue},
						),
					},
					Spec: kruisev1b1.StatefulSetSpec{
						Replicas: ptr.To(int32(3)),
					},
				},
			},
			expectError: false,
			expectedSTS: 2,
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

			result, err := reconciler.GetStatefulSetsWithFallback(ctx, namespace, clusterName)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, result)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Len(t, result.Items, tt.expectedSTS)

			if tt.expectedSTS == 1 && tt.expectedName != "" {
				assert.Equal(t, tt.expectedName, result.Items[0].Name)
				if tt.expectedReplicas > 0 {
					require.NotNil(t, result.Items[0].Spec.Replicas)
					assert.Equal(t, tt.expectedReplicas, *result.Items[0].Spec.Replicas)
				}
			}
		})
	}
}

func TestEnsureWorkerTopologyConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, kruisev1b1.AddToScheme(scheme))

	ctx := context.Background()
	namespace := "test-namespace"
	clusterName := "test-cluster"

	tests := []struct {
		name                  string
		existingObjs          []client.Object
		expectError           bool
		errorContains         string
		expectConfigMapCreate bool
		expectJailedCreate    bool
	}{
		{
			name:                  "Neither ConfigMap nor JailedConfig exist",
			existingObjs:          []client.Object{},
			expectError:           false,
			expectConfigMapCreate: true,
			expectJailedCreate:    true,
		},
		{
			name: "Only ConfigMap exists",
			existingObjs: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      consts.ConfigMapNameTopologyConfig,
						Namespace: namespace,
					},
					Data: map[string]string{
						consts.ConfigMapKeyTopologyConfig: "test-config",
					},
				},
			},
			expectError:           false,
			expectConfigMapCreate: false,
			expectJailedCreate:    true,
		},
		{
			name: "Only JailedConfig exists",
			existingObjs: []client.Object{
				&v1alpha1.JailedConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      consts.ConfigMapNameTopologyConfig,
						Namespace: namespace,
					},
					Spec: v1alpha1.JailedConfigSpec{
						ConfigMap: v1alpha1.ConfigMapReference{
							Name: consts.ConfigMapNameTopologyConfig,
						},
					},
				},
			},
			expectError:           false,
			expectConfigMapCreate: true,
			expectJailedCreate:    false,
		},
		{
			name: "Both ConfigMap and JailedConfig exist",
			existingObjs: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      consts.ConfigMapNameTopologyConfig,
						Namespace: namespace,
					},
					Data: map[string]string{
						consts.ConfigMapKeyTopologyConfig: "test-config",
					},
				},
				&v1alpha1.JailedConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      consts.ConfigMapNameTopologyConfig,
						Namespace: namespace,
					},
					Spec: v1alpha1.JailedConfigSpec{
						ConfigMap: v1alpha1.ConfigMapReference{
							Name: consts.ConfigMapNameTopologyConfig,
						},
					},
				},
			},
			expectError:           false,
			expectConfigMapCreate: false,
			expectJailedCreate:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Add SlurmCluster for fallback StatefulSet creation
			slurmCluster := &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: slurmv1.SlurmClusterSpec{
					SlurmNodes: slurmv1.SlurmNodes{
						Worker: &slurmv1.SlurmNodeWorker{
							SlurmNode: slurmv1.SlurmNode{Size: 2},
						},
					},
				},
			}

			allObjs := append(tt.existingObjs, slurmCluster)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(allObjs...).
				Build()

			reconciler := &tc.WorkerTopologyReconciler{
				BaseReconciler: tc.BaseReconciler{
					Client: fakeClient,
					Scheme: scheme,
				},
			}

			logger := log.Log.WithName("test")
			result, err := reconciler.EnsureWorkerTopologyConfigMap(ctx, namespace, clusterName, false, logger)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, result)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			// Verify ConfigMap exists
			configMap := &corev1.ConfigMap{}
			err = fakeClient.Get(ctx, client.ObjectKey{
				Name:      consts.ConfigMapNameTopologyConfig,
				Namespace: namespace,
			}, configMap)
			require.NoError(t, err)
			assert.NotEmpty(t, configMap.Data[consts.ConfigMapKeyTopologyConfig])

			// Verify JailedConfig exists
			jailedConfig := &v1alpha1.JailedConfig{}
			err = fakeClient.Get(ctx, client.ObjectKey{
				Name:      consts.ConfigMapNameTopologyConfig,
				Namespace: namespace,
			}, jailedConfig)
			require.NoError(t, err)
			assert.Equal(t, consts.ConfigMapNameTopologyConfig, jailedConfig.Spec.ConfigMap.Name)
		})
	}
}

func TestFilterPodsExcludingEphemeralNodeSets(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}

	tests := []struct {
		name              string
		pods              []corev1.Pod
		ephemeralNodeSets map[string]struct{}
		expectedPodNames  []string
	}{
		{
			name: "No ephemeral NodeSets - all pods included",
			pods: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-1"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-2"}}},
			},
			ephemeralNodeSets: map[string]struct{}{},
			expectedPodNames:  []string{"pod-1", "pod-2"},
		},
		{
			name: "Filter out pods from ephemeral NodeSets",
			pods: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-static-1", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-static"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-ephemeral-1", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-ephemeral"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-static-2", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-static"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-ephemeral-2", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-ephemeral"}}},
			},
			ephemeralNodeSets: map[string]struct{}{
				"nodeset-ephemeral": {},
			},
			expectedPodNames: []string{"pod-static-1", "pod-static-2"},
		},
		{
			name: "Pods without NodeSet label are included",
			pods: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-with-label", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-ephemeral"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-without-label", Labels: map[string]string{}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-nil-labels"}},
			},
			ephemeralNodeSets: map[string]struct{}{
				"nodeset-ephemeral": {},
			},
			expectedPodNames: []string{"pod-without-label", "pod-nil-labels"},
		},
		{
			name: "All pods from ephemeral NodeSets filtered out",
			pods: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-ephemeral-1"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-ephemeral-2"}}},
			},
			ephemeralNodeSets: map[string]struct{}{
				"nodeset-ephemeral-1": {},
				"nodeset-ephemeral-2": {},
			},
			expectedPodNames: []string{},
		},
		{
			name:              "Empty pods list",
			pods:              []corev1.Pod{},
			ephemeralNodeSets: map[string]struct{}{"nodeset-ephemeral": {}},
			expectedPodNames:  []string{},
		},
		{
			name: "Multiple ephemeral NodeSets",
			pods: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-static", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-static"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-eph-1", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-eph-1"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-eph-2", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-eph-2"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-eph-3", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-eph-3"}}},
			},
			ephemeralNodeSets: map[string]struct{}{
				"nodeset-eph-1": {},
				"nodeset-eph-2": {},
				"nodeset-eph-3": {},
			},
			expectedPodNames: []string{"pod-static"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.FilterPodsExcludingEphemeralNodeSets(tt.pods, tt.ephemeralNodeSets)

			resultNames := make([]string, 0, len(result))
			for _, pod := range result {
				resultNames = append(resultNames, pod.Name)
			}

			assert.Equal(t, tt.expectedPodNames, resultNames)
		})
	}
}

func TestFilterStatefulSetsExcludingEphemeralNodeSets(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}

	tests := []struct {
		name              string
		statefulSets      []kruisev1b1.StatefulSet
		ephemeralNodeSets map[string]struct{}
		expectedNames     []string
	}{
		{
			name: "No ephemeral NodeSets - all StatefulSets included",
			statefulSets: []kruisev1b1.StatefulSet{
				{ObjectMeta: metav1.ObjectMeta{Name: "sts-1", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-1"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "sts-2", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-2"}}},
			},
			ephemeralNodeSets: map[string]struct{}{},
			expectedNames:     []string{"sts-1", "sts-2"},
		},
		{
			name: "Filter out StatefulSets from ephemeral NodeSets",
			statefulSets: []kruisev1b1.StatefulSet{
				{ObjectMeta: metav1.ObjectMeta{Name: "sts-static", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-static"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "sts-ephemeral", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-ephemeral"}}},
			},
			ephemeralNodeSets: map[string]struct{}{
				"nodeset-ephemeral": {},
			},
			expectedNames: []string{"sts-static"},
		},
		{
			name: "StatefulSets without NodeSet label are included",
			statefulSets: []kruisev1b1.StatefulSet{
				{ObjectMeta: metav1.ObjectMeta{Name: "sts-with-label", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-ephemeral"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "sts-without-label", Labels: map[string]string{}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "sts-nil-labels"}},
			},
			ephemeralNodeSets: map[string]struct{}{
				"nodeset-ephemeral": {},
			},
			expectedNames: []string{"sts-without-label", "sts-nil-labels"},
		},
		{
			name: "All StatefulSets from ephemeral NodeSets filtered out",
			statefulSets: []kruisev1b1.StatefulSet{
				{ObjectMeta: metav1.ObjectMeta{Name: "sts-1", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-eph-1"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "sts-2", Labels: map[string]string{consts.LabelNodeSetKey: "nodeset-eph-2"}}},
			},
			ephemeralNodeSets: map[string]struct{}{
				"nodeset-eph-1": {},
				"nodeset-eph-2": {},
			},
			expectedNames: []string{},
		},
		{
			name:              "Empty StatefulSets list",
			statefulSets:      []kruisev1b1.StatefulSet{},
			ephemeralNodeSets: map[string]struct{}{"nodeset-ephemeral": {}},
			expectedNames:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.FilterStatefulSetsExcludingEphemeralNodeSets(tt.statefulSets, tt.ephemeralNodeSets)

			resultNames := make([]string, 0, len(result))
			for _, sts := range result {
				resultNames = append(resultNames, sts.Name)
			}

			assert.Equal(t, tt.expectedNames, resultNames)
		})
	}
}
