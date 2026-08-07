package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/soperator-e2e/acceptance/framework"
	"nebius.ai/soperator-e2e/acceptance/internal/kubeobjects"
)

func TestDiscoveredNodeSetsFromLiveList(t *testing.T) {
	nodeSets := kubeobjects.NodeSetList{
		Items: []kubeobjects.NodeSet{
			{
				Metadata: kubeobjects.ObjectMeta{Name: "worker-gpu"},
				Spec: kubeobjects.NodeSetSpec{
					ClusterName: "soperator",
					Replicas:    2,
					GPU:         kubeobjects.NodeSetGPUSpec{Enabled: true},
				},
			},
			{
				Metadata: kubeobjects.ObjectMeta{Name: "worker-cpu"},
				Spec: kubeobjects.NodeSetSpec{
					ClusterName: "soperator",
					Replicas:    3,
				},
			},
			{
				Metadata: kubeobjects.ObjectMeta{Name: "other-worker"},
				Spec: kubeobjects.NodeSetSpec{
					ClusterName: "other",
					Replicas:    4,
					GPU:         kubeobjects.NodeSetGPUSpec{Enabled: true},
				},
			},
		},
	}

	discovered := DiscoveredNodeSetsFromLiveList(nodeSets, "soperator")
	require.Len(t, discovered, 2)

	assert.Equal(t, "worker-cpu", discovered[0].Name)
	assert.Equal(t, 3, discovered[0].Size)
	assert.False(t, discovered[0].HasGPU)

	assert.Equal(t, "worker-gpu", discovered[1].Name)
	assert.Equal(t, 2, discovered[1].Size)
	assert.True(t, discovered[1].HasGPU)
}

func TestDiscoveredNodeSetsFromLiveListDoesNotFilterWhenClusterNameIsEmpty(t *testing.T) {
	nodeSets := kubeobjects.NodeSetList{
		Items: []kubeobjects.NodeSet{
			{
				Metadata: kubeobjects.ObjectMeta{Name: "worker-gpu"},
				Spec: kubeobjects.NodeSetSpec{
					ClusterName: "soperator",
					Replicas:    2,
					GPU:         kubeobjects.NodeSetGPUSpec{Enabled: true},
				},
			},
			{
				Metadata: kubeobjects.ObjectMeta{Name: "worker-cpu"},
				Spec: kubeobjects.NodeSetSpec{
					ClusterName: "soperator",
					Replicas:    3,
				},
			},
		},
	}

	discovered := DiscoveredNodeSetsFromLiveList(nodeSets, "")
	require.Len(t, discovered, 2)

	assert.Equal(t, "worker-cpu", discovered[0].Name)
	assert.Equal(t, "worker-gpu", discovered[1].Name)
}

func TestWorkerPodsBySlurmNodeName(t *testing.T) {
	pods := corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "kube-worker-a"},
				Spec:       corev1.PodSpec{Hostname: "worker-a"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "kube-worker-b"},
				Spec:       corev1.PodSpec{Hostname: "worker-b"},
			},
		},
	}

	discovered, err := WorkerPodsBySlurmNodeName(pods)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"worker-a": "kube-worker-a",
		"worker-b": "kube-worker-b",
	}, discovered)
}

func TestWorkerPodsBySlurmNodeNameRejectsMissingHostname(t *testing.T) {
	_, err := WorkerPodsBySlurmNodeName(corev1.PodList{
		Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "worker-0"}}},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "empty spec.hostname")
}

func TestWorkerPodsBySlurmNodeNameRejectsDuplicateHostname(t *testing.T) {
	_, err := WorkerPodsBySlurmNodeName(corev1.PodList{
		Items: []corev1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Name: "worker-a-0"}, Spec: corev1.PodSpec{Hostname: "worker-0"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "worker-b-0"}, Spec: corev1.PodSpec{Hostname: "worker-0"}},
		},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "both declare spec.hostname=worker-0")
}

func TestClassifyWorkersSeparatesCPUAndGPU(t *testing.T) {
	state := &framework.ClusterState{
		Workers: []framework.WorkerRef{
			{Name: "worker-gpu-0"},
			{Name: "worker-cpu-0"},
			{Name: "worker-gpu-1"},
		},
		DiscoveredNodeSets: []framework.DiscoveredNodeSet{
			{Name: "worker-gpu", Size: 2, HasGPU: true},
			{Name: "worker-cpu", Size: 1, HasGPU: false},
		},
	}

	ClassifyWorkers(state)

	assert.ElementsMatch(t, []framework.WorkerRef{{Name: "worker-cpu-0"}}, state.CPUWorkers)
	assert.ElementsMatch(t, []framework.WorkerRef{{Name: "worker-gpu-0"}, {Name: "worker-gpu-1"}}, state.GPUWorkers)
	assert.ElementsMatch(t, []framework.WorkerRef{{Name: "worker-cpu-0"}}, state.WorkersByNodeSet["worker-cpu"])
	assert.ElementsMatch(t, []framework.WorkerRef{{Name: "worker-gpu-0"}, {Name: "worker-gpu-1"}}, state.WorkersByNodeSet["worker-gpu"])
}
