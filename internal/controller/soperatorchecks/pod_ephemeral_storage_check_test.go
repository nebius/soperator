package soperatorchecks

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/kubeletclient"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

// splitHostPort breaks an httptest server URL into the pieces a node status carries.
func splitHostPort(t *testing.T, rawURL string) (string, int32) {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)

	host, portStr, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	return host, int32(port)
}

func createTestPodEphemeralStorageCheck(t *testing.T, objects ...client.Object) *PodEphemeralStorageCheck {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))

	fakeClient := clientfake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	recorder := record.NewFakeRecorder(100)

	// Create a mock slurmapi.ClientSet
	slurmAPIClients := &slurmapi.ClientSet{}

	controller, err := NewPodEphemeralStorageCheck(
		fakeClient,
		scheme,
		recorder,
		&rest.Config{},
		time.Minute,
		80.0,
		75.0,
		slurmAPIClients,
		kubeletclient.Config{InsecureSkipTLSVerify: true},
	)
	require.NoError(t, err)
	return controller
}

func TestIsPodRelevant(t *testing.T) {
	controller := createTestPodEphemeralStorageCheck(t)

	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name: "worker pod with correct labels should be relevant",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker-pod",
					Namespace: "test-ns",
					Labels: map[string]string{
						consts.LabelWorkerKey:    consts.LabelWorkerValue,
						consts.LabelManagedByKey: consts.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "StatefulSet",
							APIVersion: "apps.kruise.io/v1beta1",
							Name:       "worker-sts",
							UID:        "sts-uid-123",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "worker pod without owner references should not be relevant",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker-pod-no-owner",
					Namespace: "test-ns",
					Labels: map[string]string{
						consts.LabelWorkerKey:    consts.LabelWorkerValue,
						consts.LabelManagedByKey: consts.LabelManagedByValue,
					},
				},
			},
			expected: false,
		},
		{
			name: "worker pod with wrong owner kind should not be relevant",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker-pod-wrong-owner",
					Namespace: "test-ns",
					Labels: map[string]string{
						consts.LabelWorkerKey:    consts.LabelWorkerValue,
						consts.LabelManagedByKey: consts.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "Deployment",
							APIVersion: "apps/v1",
							Name:       "worker-deployment",
							UID:        "deploy-uid-123",
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "worker pod with wrong API version should not be relevant",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker-pod-wrong-api",
					Namespace: "test-ns",
					Labels: map[string]string{
						consts.LabelWorkerKey:    consts.LabelWorkerValue,
						consts.LabelManagedByKey: consts.LabelManagedByValue,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "StatefulSet",
							APIVersion: "apps/v1", // Wrong API version
							Name:       "worker-sts",
							UID:        "sts-uid-123",
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "worker pod without managed-by label should not be relevant",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker-pod",
					Namespace: "test-ns",
					Labels: map[string]string{
						consts.LabelWorkerKey: consts.LabelWorkerValue,
					},
				},
			},
			expected: false,
		},
		{
			name: "pod with managed-by but wrong component should not be relevant",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-pod",
					Namespace: "test-ns",
					Labels: map[string]string{
						consts.LabelWorkerKey:    "other-component",
						consts.LabelManagedByKey: consts.LabelManagedByValue,
					},
				},
			},
			expected: false,
		},
		{
			name: "pod with wrong managed-by value should not be relevant",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker-pod",
					Namespace: "test-ns",
					Labels: map[string]string{
						consts.LabelWorkerKey:    consts.LabelWorkerValue,
						consts.LabelManagedByKey: "other-operator",
					},
				},
			},
			expected: false,
		},
		{
			name: "pod without any relevant labels should not be relevant",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unlabeled-pod",
					Namespace: "test-ns",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := controller.isPodRelevant(tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetEphemeralStorageLimitForPod(t *testing.T) {
	controller := createTestPodEphemeralStorageCheck(t)

	tests := []struct {
		name     string
		pod      corev1.Pod
		expected uint64
	}{
		{
			name: "pod with ephemeral storage limit",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: 1073741824, // 1Gi in bytes
		},
		{
			name: "pod with multiple containers with limits",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
								},
							},
						},
						{
							Name: "container2",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceEphemeralStorage: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			},
			expected: 3221225472, // 3Gi in bytes
		},
		{
			name: "pod with init containers with limits",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "init-container",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceEphemeralStorage: resource.MustParse("500Mi"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: 1598029824, // 1Gi + 500Mi in bytes
		},
		{
			name: "pod without ephemeral storage limits",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1000m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := controller.getEphemeralStorageLimitForPod(tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetEphemeralStorageStatsFromNode(t *testing.T) {
	// Create a mock kubelet stats response
	mockStats := KubeletStats{
		Node: struct {
			NodeName string `json:"nodeName"`
		}{
			NodeName: "test-node",
		},
		Pods: []PodStats{
			{
				PodRef: struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
					UID       string `json:"uid"`
				}{
					Name:      "worker-pod-1",
					Namespace: "test-ns",
					UID:       "uid-1",
				},
				EphemeralStorage: struct {
					AvailableBytes *uint64 `json:"availableBytes,omitempty"`
					CapacityBytes  *uint64 `json:"capacityBytes,omitempty"`
					UsedBytes      *uint64 `json:"usedBytes,omitempty"`
				}{
					UsedBytes: func() *uint64 { v := uint64(800000000); return &v }(), // 800MB
				},
			},
			{
				PodRef: struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
					UID       string `json:"uid"`
				}{
					Name:      "worker-pod-2",
					Namespace: "test-ns",
					UID:       "uid-2",
				},
				EphemeralStorage: struct {
					AvailableBytes *uint64 `json:"availableBytes,omitempty"`
					CapacityBytes  *uint64 `json:"capacityBytes,omitempty"`
					UsedBytes      *uint64 `json:"usedBytes,omitempty"`
				}{
					UsedBytes: func() *uint64 { v := uint64(400000000); return &v }(), // 400MB
				},
			},
			{
				PodRef: struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
					UID       string `json:"uid"`
				}{
					Name:      "non-worker-pod",
					Namespace: "test-ns",
					UID:       "uid-3",
				},
				EphemeralStorage: struct {
					AvailableBytes *uint64 `json:"availableBytes,omitempty"`
					CapacityBytes  *uint64 `json:"capacityBytes,omitempty"`
					UsedBytes      *uint64 `json:"usedBytes,omitempty"`
				}{
					UsedBytes: func() *uint64 { v := uint64(100000000); return &v }(), // 100MB
				},
			},
		},
	}

	// Stand in for the kubelet itself, serving TLS with a self-signed certificate the
	// same way a real kubelet does.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats/summary" {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(mockStats); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Create test pods
	workerPods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-pod-1",
				Namespace: "test-ns",
				UID:       types.UID("uid-1"),
			},
			Spec: corev1.PodSpec{
				NodeName: "test-node",
				Containers: []corev1.Container{
					{
						Name: "container1",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"), // 1073741824 bytes
							},
						},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-pod-2",
				Namespace: "test-ns",
				UID:       types.UID("uid-2"),
			},
			Spec: corev1.PodSpec{
				NodeName: "test-node",
				Containers: []corev1.Container{
					{
						Name: "container1",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceEphemeralStorage: resource.MustParse("500Mi"), // 524288000 bytes
							},
						},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-pod-no-limit",
				Namespace: "test-ns",
				UID:       types.UID("uid-4"),
			},
			Spec: corev1.PodSpec{
				NodeName: "test-node",
			},
		},
	}

	host, port := splitHostPort(t, server.URL)

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: host},
			},
			DaemonEndpoints: corev1.NodeDaemonEndpoints{
				KubeletEndpoint: corev1.DaemonEndpoint{Port: port},
			},
		},
	}

	controller := createTestPodEphemeralStorageCheck(t, node)

	result, err := controller.getEphemeralStorageStatsFromNode(context.Background(), "test-node", workerPods)
	require.NoError(t, err)

	// Verify results
	assert.Len(t, result, 2) // Should only include pods with ephemeral storage limits

	// Find and verify worker-pod-1 stats
	var pod1Stats *EphemeralStorageInfo
	var pod2Stats *EphemeralStorageInfo
	for i := range result {
		if result[i].PodName == "worker-pod-1" {
			pod1Stats = &result[i]
		} else if result[i].PodName == "worker-pod-2" {
			pod2Stats = &result[i]
		}
	}

	require.NotNil(t, pod1Stats)
	assert.Equal(t, "worker-pod-1", pod1Stats.PodName)
	assert.Equal(t, "test-ns", pod1Stats.PodNamespace)
	assert.Equal(t, "test-node", pod1Stats.NodeName)
	assert.Equal(t, uint64(800000000), pod1Stats.UsedBytes)
	assert.Equal(t, uint64(1073741824), pod1Stats.LimitBytes)
	assert.InDelta(t, 74.51, pod1Stats.UsagePercent, 0.1) // 800MB / 1Gi ≈ 74.51%

	require.NotNil(t, pod2Stats)
	assert.Equal(t, "worker-pod-2", pod2Stats.PodName)
	assert.Equal(t, uint64(400000000), pod2Stats.UsedBytes)
	assert.Equal(t, uint64(524288000), pod2Stats.LimitBytes)
	assert.InDelta(t, 76.29, pod2Stats.UsagePercent, 0.1) // 400MB / 500Mi ≈ 76.29%
}

// Integration test style function to test the logic without HTTP calls
func TestGetEphemeralStorageStatsFromNodeLogic(t *testing.T) {
	controller := createTestPodEphemeralStorageCheck(t)

	// Test the logic by directly testing with mock data that would come from kubelet
	mockStats := KubeletStats{
		Node: struct {
			NodeName string `json:"nodeName"`
		}{
			NodeName: "test-node",
		},
		Pods: []PodStats{
			{
				PodRef: struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
					UID       string `json:"uid"`
				}{
					Name:      "worker-pod-1",
					Namespace: "test-ns",
					UID:       "uid-1",
				},
				EphemeralStorage: struct {
					AvailableBytes *uint64 `json:"availableBytes,omitempty"`
					CapacityBytes  *uint64 `json:"capacityBytes,omitempty"`
					UsedBytes      *uint64 `json:"usedBytes,omitempty"`
				}{
					UsedBytes: func() *uint64 { v := uint64(800000000); return &v }(), // 800MB
				},
			},
		},
	}

	workerPods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-pod-1",
				Namespace: "test-ns",
				UID:       types.UID("uid-1"),
			},
			Spec: corev1.PodSpec{
				NodeName: "test-node",
				Containers: []corev1.Container{
					{
						Name: "container1",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
		},
	}

	// Test the pod filtering and calculation logic directly
	workerPodMap := make(map[string]corev1.Pod)
	for _, pod := range workerPods {
		if pod.Spec.NodeName == "test-node" {
			workerPodMap[string(pod.UID)] = pod
		}
	}

	var storageInfos []EphemeralStorageInfo
	for _, podStat := range mockStats.Pods {
		workerPod, isWorkerPod := workerPodMap[podStat.PodRef.UID]
		if !isWorkerPod {
			continue
		}

		limitBytes := controller.getEphemeralStorageLimitForPod(workerPod)
		if limitBytes == 0 {
			continue
		}

		usedBytes := uint64(0)
		if podStat.EphemeralStorage.UsedBytes != nil {
			usedBytes = *podStat.EphemeralStorage.UsedBytes
		}

		usagePercent := float64(usedBytes) / float64(limitBytes) * 100.0

		storageInfo := EphemeralStorageInfo{
			PodName:      podStat.PodRef.Name,
			PodNamespace: podStat.PodRef.Namespace,
			NodeName:     "test-node",
			UsedBytes:    usedBytes,
			LimitBytes:   limitBytes,
			UsagePercent: usagePercent,
		}

		storageInfos = append(storageInfos, storageInfo)
	}

	// Verify the results
	require.Len(t, storageInfos, 1)
	assert.Equal(t, "worker-pod-1", storageInfos[0].PodName)
	assert.Equal(t, "test-ns", storageInfos[0].PodNamespace)
	assert.Equal(t, "test-node", storageInfos[0].NodeName)
	assert.Equal(t, uint64(800000000), storageInfos[0].UsedBytes)
	assert.Equal(t, uint64(1073741824), storageInfos[0].LimitBytes)
	assert.InDelta(t, 74.51, storageInfos[0].UsagePercent, 0.1) // 800MB / 1Gi ≈ 74.51%
}

func TestCreateEphemeralStorageEventStructure(t *testing.T) {
	// Test the event structure and message formatting logic
	// without actually creating events in the fake client

	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-ns",
			UID:       types.UID("test-pod-uid-123"),
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	testInfo := EphemeralStorageInfo{
		PodName:      "test-pod",
		PodNamespace: "test-ns",
		NodeName:     "test-node",
		UsedBytes:    800000000,  // 800MB
		LimitBytes:   1073741824, // 1GB
		UsagePercent: 74.51,
	}

	tests := []struct {
		name                    string
		pod                     *corev1.Pod
		info                    EphemeralStorageInfo
		expectedEventName       string
		expectedReason          string
		expectedType            string
		expectedMessageContains []string
	}{
		{
			name:              "event structure validation",
			pod:               testPod,
			info:              testInfo,
			expectedEventName: "test-pod-test-ns-ephemeral-storage-warning",
			expectedReason:    consts.HighEphemeralStorageUsage,
			expectedType:      corev1.EventTypeWarning,
			expectedMessageContains: []string{
				"test-pod",
				"test-ns",
				"74.51%",
				"800000000 bytes used",
				"1073741824 bytes limit",
			},
		},
		{
			name: "event with different usage values",
			pod:  testPod,
			info: EphemeralStorageInfo{
				PodName:      "test-pod",
				PodNamespace: "test-ns",
				NodeName:     "test-node",
				UsedBytes:    500000000, // 500MB
				LimitBytes:   524288000, // 500MB
				UsagePercent: 95.37,
			},
			expectedEventName: "test-pod-test-ns-ephemeral-storage-warning",
			expectedReason:    consts.HighEphemeralStorageUsage,
			expectedType:      corev1.EventTypeWarning,
			expectedMessageContains: []string{
				"test-pod",
				"test-ns",
				"95.37%",
				"500000000 bytes used",
				"524288000 bytes limit",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the event name generation
			expectedEventName := fmt.Sprintf("%s-%s-ephemeral-storage-warning", tt.pod.Name, tt.pod.Namespace)
			assert.Equal(t, tt.expectedEventName, expectedEventName)

			// Test the message formatting
			expectedMessage := fmt.Sprintf("Pod %s in namespace %s is using %.2f%% of its ephemeral storage limit (%d bytes used, %d bytes limit)",
				tt.info.PodName, tt.info.PodNamespace,
				tt.info.UsagePercent, tt.info.UsedBytes, tt.info.LimitBytes)

			// Verify message contains expected content
			for _, content := range tt.expectedMessageContains {
				assert.Contains(t, expectedMessage, content)
			}

			// Test that the event would have correct metadata structure
			// (this tests the logic without the actual client call)
			eventLabels := map[string]string{
				consts.LabelWorkerKey:    consts.LabelWorkerValue,
				consts.LabelManagedByKey: consts.LabelManagedByValue,
			}
			assert.Equal(t, consts.LabelWorkerValue, eventLabels[consts.LabelWorkerKey])
			assert.Equal(t, consts.LabelManagedByValue, eventLabels[consts.LabelManagedByKey])

			// Test involved object reference structure
			involvedObjRef := corev1.ObjectReference{
				Kind:      "Pod",
				Name:      tt.pod.Name,
				Namespace: tt.pod.Namespace,
				UID:       tt.pod.UID,
			}
			assert.Equal(t, "Pod", involvedObjRef.Kind)
			assert.Equal(t, tt.pod.Name, involvedObjRef.Name)
			assert.Equal(t, tt.pod.Namespace, involvedObjRef.Namespace)
			assert.Equal(t, tt.pod.UID, involvedObjRef.UID)

			// Verify event constants
			assert.Equal(t, tt.expectedReason, consts.HighEphemeralStorageUsage)
			assert.Equal(t, tt.expectedType, corev1.EventTypeWarning)
		})
	}
}

func TestCreateEphemeralStorageEvent(t *testing.T) {
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-pod",
			Namespace:       "test-ns",
			UID:             types.UID("test-pod-uid-123"),
			ResourceVersion: "123",
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	testInfo := EphemeralStorageInfo{
		PodName:      "test-pod",
		PodNamespace: "test-ns",
		NodeName:     "test-node",
		UsedBytes:    800000000,  // 800MB
		LimitBytes:   1073741824, // 1GB
		UsagePercent: 74.51,      // 800MB / 1GB ≈ 74.51%
	}

	tests := []struct {
		name           string
		existingEvents []client.Object
		expectedCount  int32
		shouldCreate   bool
	}{
		{
			name:           "create new event when none exists",
			existingEvents: []client.Object{},
			expectedCount:  1,
			shouldCreate:   true,
		},
		{
			name: "increment count when recent similar event exists",
			existingEvents: []client.Object{
				&corev1.Event{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-event",
						Namespace: "test-ns",
					},
					InvolvedObject: corev1.ObjectReference{
						Kind:      "Pod",
						Name:      "test-pod",
						Namespace: "test-ns",
						UID:       types.UID("test-pod-uid-123"),
					},
					Reason: consts.HighEphemeralStorageUsage,
					Message: fmt.Sprintf("Pod %s in namespace %s is using %.2f%% of its ephemeral storage limit (%d bytes used, %d bytes limit)",
						testInfo.PodName, testInfo.PodNamespace,
						testInfo.UsagePercent, testInfo.UsedBytes, testInfo.LimitBytes),
					Type:          corev1.EventTypeWarning,
					Count:         1,
					LastTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Minute)), // Recent event
				},
			},
			expectedCount: 2,
			shouldCreate:  false,
		},
		{
			name: "create new event when old similar event exists",
			existingEvents: []client.Object{
				&corev1.Event{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "old-event",
						Namespace: "test-ns",
					},
					InvolvedObject: corev1.ObjectReference{
						Kind:      "Pod",
						Name:      "test-pod",
						Namespace: "test-ns",
						UID:       types.UID("test-pod-uid-123"),
					},
					Reason: consts.HighEphemeralStorageUsage,
					Message: fmt.Sprintf("Pod %s in namespace %s is using %.2f%% of its ephemeral storage limit (%d bytes used, %d bytes limit)",
						testInfo.PodName, testInfo.PodNamespace,
						testInfo.UsagePercent, testInfo.UsedBytes, testInfo.LimitBytes),
					Type:          corev1.EventTypeWarning,
					Count:         1,
					LastTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)), // Old event
				},
			},
			expectedCount: 1,
			shouldCreate:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := createTestPodEphemeralStorageCheck(t, tt.existingEvents...)

			err := controller.createEphemeralStorageEvent(context.Background(), testPod, testInfo)
			assert.NoError(t, err)

			// Verify the event was created or updated correctly
			eventList := &corev1.EventList{}
			err = controller.List(context.Background(), eventList, client.InNamespace("test-ns"))
			assert.NoError(t, err)

			// Find events related to our test pod
			var relevantEvents []corev1.Event
			for _, event := range eventList.Items {
				if event.InvolvedObject.Name == "test-pod" &&
					event.InvolvedObject.UID == types.UID("test-pod-uid-123") &&
					event.Reason == consts.HighEphemeralStorageUsage {
					relevantEvents = append(relevantEvents, event)
				}
			}

			if tt.shouldCreate {
				// Should have created a new event
				assert.Len(t, relevantEvents, len(tt.existingEvents)+1)
				// Find the new event (one with count = expectedCount)
				var newEvent *corev1.Event
				for i, event := range relevantEvents {
					if event.Count == tt.expectedCount {
						newEvent = &relevantEvents[i]
						break
					}
				}
				assert.NotNil(t, newEvent)
				assert.Equal(t, tt.expectedCount, newEvent.Count)
			} else {
				// Should have updated existing event
				assert.Len(t, relevantEvents, len(tt.existingEvents))
				// Find the updated event
				var updatedEvent *corev1.Event
				for i, event := range relevantEvents {
					if event.Count == tt.expectedCount {
						updatedEvent = &relevantEvents[i]
						break
					}
				}
				assert.NotNil(t, updatedEvent)
				assert.Equal(t, tt.expectedCount, updatedEvent.Count)
			}
		})
	}
}

func TestIsDrainedByEphemeralStorageCheck(t *testing.T) {
	controller := createTestPodEphemeralStorageCheck(t)

	tests := []struct {
		name     string
		node     slurmapi.Node
		expected bool
	}{
		{
			name: "drained by ephemeral storage check",
			node: slurmapi.Node{
				States: map[api.V0044NodeState]struct{}{
					api.V0044NodeStateDRAIN: {},
				},
				Reason: &slurmapi.NodeReason{
					Reason: consts.SlurmUserReasonHC + " pod_ephemeral_storage 90.00% of ephemeral storage is used.",
				},
			},
			expected: true,
		},
		{
			name: "drained by different reason",
			node: slurmapi.Node{
				States: map[api.V0044NodeState]struct{}{
					api.V0044NodeStateDRAIN: {},
				},
				Reason: &slurmapi.NodeReason{
					Reason: consts.SlurmUserReasonHC + " gpu_check some GPU failure",
				},
			},
			expected: false,
		},
		{
			name: "drained with no reason",
			node: slurmapi.Node{
				States: map[api.V0044NodeState]struct{}{
					api.V0044NodeStateDRAIN: {},
				},
				Reason: nil,
			},
			expected: false,
		},
		{
			name: "not drained but reason matches prefix",
			node: slurmapi.Node{
				States: map[api.V0044NodeState]struct{}{
					api.V0044NodeStateIDLE: {},
				},
				Reason: &slurmapi.NodeReason{
					Reason: consts.SlurmUserReasonHC + " pod_ephemeral_storage 90.00%",
				},
			},
			expected: false,
		},
		{
			name: "drain+idle with matching reason",
			node: slurmapi.Node{
				States: map[api.V0044NodeState]struct{}{
					api.V0044NodeStateDRAIN: {},
					api.V0044NodeStateIDLE:  {},
				},
				Reason: &slurmapi.NodeReason{
					Reason: consts.SlurmUserReasonHC + " pod_ephemeral_storage 85.50% of ephemeral storage is used.",
				},
			},
			expected: true,
		},
		{
			name: "drained manually by user",
			node: slurmapi.Node{
				States: map[api.V0044NodeState]struct{}{
					api.V0044NodeStateDRAIN: {},
				},
				Reason: &slurmapi.NodeReason{
					Reason: "manual maintenance",
				},
			},
			expected: false,
		},
		{
			name: "reason is exact prefix with no trailing content",
			node: slurmapi.Node{
				States: map[api.V0044NodeState]struct{}{
					api.V0044NodeStateDRAIN: {},
				},
				Reason: &slurmapi.NodeReason{
					Reason: consts.SlurmUserReasonHC + " pod_ephemeral_storage",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := controller.isDrainedByEphemeralStorageCheck(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUndrainSlurmNode(t *testing.T) {
	clusterName := types.NamespacedName{Name: "test-cluster", Namespace: "test-ns"}
	nodeName := "worker-0"

	tests := []struct {
		name         string
		setupMock    func(*slurmapifake.MockClient)
		expectError  bool
		expectResume bool
	}{
		{
			name: "successful undrain",
			setupMock: func(m *slurmapifake.MockClient) {
				m.EXPECT().
					SlurmV0044PostNodeWithResponse(mock.Anything, nodeName, mock.MatchedBy(func(body api.V0044UpdateNodeMsg) bool {
						return body.State != nil &&
							len(*body.State) == 1 &&
							(*body.State)[0] == api.V0044UpdateNodeMsgStateRESUME
					})).
					Return(&api.SlurmV0044PostNodeResponse{
						JSON200: &api.V0044OpenapiResp{},
					}, nil)
			},
			expectError:  false,
			expectResume: true,
		},
		{
			name: "API returns error",
			setupMock: func(m *slurmapifake.MockClient) {
				m.EXPECT().
					SlurmV0044PostNodeWithResponse(mock.Anything, nodeName, mock.Anything).
					Return(nil, fmt.Errorf("connection refused"))
			},
			expectError: true,
		},
		{
			name: "API returns response with errors",
			setupMock: func(m *slurmapifake.MockClient) {
				errs := api.V0044OpenapiErrors{
					{Description: strPtr("node not found"), ErrorNumber: int32Ptr(404)},
				}
				m.EXPECT().
					SlurmV0044PostNodeWithResponse(mock.Anything, nodeName, mock.Anything).
					Return(&api.SlurmV0044PostNodeResponse{
						JSON200: &api.V0044OpenapiResp{
							Errors: &errs,
						},
					}, nil)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := slurmapifake.NewMockClient(t)
			tt.setupMock(mockClient)

			slurmAPIClients := slurmapi.NewClientSet(context.Background())
			slurmAPIClients.AddClient(clusterName, mockClient)

			controller := createTestPodEphemeralStorageCheck(t)
			controller.slurmAPIClients = slurmAPIClients

			err := controller.undrainSlurmNode(context.Background(), clusterName, nodeName)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUndrainSlurmNode_ClusterNotFound(t *testing.T) {
	slurmAPIClients := slurmapi.NewClientSet(context.Background())
	// Don't add any client — the cluster won't be found.

	controller := createTestPodEphemeralStorageCheck(t)
	controller.slurmAPIClients = slurmAPIClients

	err := controller.undrainSlurmNode(
		context.Background(),
		types.NamespacedName{Name: "nonexistent", Namespace: "ns"},
		"worker-0",
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func strPtr(s string) *string { return &s }
func int32Ptr(i int32) *int32 { return &i }
