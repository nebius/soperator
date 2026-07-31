package acceptance

import (
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/e2e/acceptance/framework"
)

const testTargetSoperatorVersion = "4.2.0"

func TestRunnerTagFilter(t *testing.T) {
	cpuAndGPUState := &framework.ClusterState{
		CPUWorkers: []framework.WorkerRef{{Name: "worker-cpu-0"}},
		GPUWorkers: []framework.WorkerRef{{Name: "worker-gpu-0"}},
	}
	noGPUState := &framework.ClusterState{}
	gpuOnlyState := &framework.ClusterState{
		GPUWorkers: []framework.WorkerRef{{Name: "worker-gpu-0"}},
	}

	tests := []struct {
		name             string
		state            *framework.ClusterState
		runUnstableTests bool
		want             string
	}{
		{
			name:  "default excludes unstable",
			state: cpuAndGPUState,
			want:  "~@unstable",
		},
		{
			name:             "run unstable has no tag filter when CPU and GPU workers exist",
			state:            cpuAndGPUState,
			runUnstableTests: true,
			want:             "",
		},
		{
			name:  "without GPU workers also excludes GPU",
			state: noGPUState,
			want:  "~@unstable && ~@gpu && ~@cpu",
		},
		{
			name:             "without CPU workers excludes CPU",
			state:            gpuOnlyState,
			runUnstableTests: true,
			want:             "~@cpu",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, err := NewRunner(Options{
				KubectlContext:            "dev-context",
				TargetSoperatorVersion:    testTargetSoperatorVersion,
				State:                     tt.state,
				ExcludeUnstable:           !tt.runUnstableTests,
				ExcludeMissingWorkerKinds: true,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, runner.tagFilter())
		})
	}
}

func TestRunnerFeaturePaths(t *testing.T) {
	features := FeatureSource{
		FS: fstest.MapFS{
			"features/sample.feature": {Data: []byte(`Feature: Sample
  @soperator_version_>=4.0.0
  Scenario: old scenario
    Then old behavior works

  @soperator_version_>=4.2.0
  Scenario: new scenario
    Then new behavior works
`)},
		},
		Paths: []string{"features/sample.feature"},
	}

	runner, err := NewRunner(Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: "4.1.5-reb85d0e5",
		Features:               features,
	})
	require.NoError(t, err)
	paths, err := runner.featurePaths()
	require.NoError(t, err)
	assert.Equal(t, []string{"features/sample.feature:3"}, paths)

	scenarios := []string{"features/sample.feature:3", "features/sample.feature:7"}
	features.Paths = scenarios
	runner, err = NewRunner(Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Features:               features,
	})
	require.NoError(t, err)
	paths, err = runner.featurePaths()
	require.NoError(t, err)
	assert.Equal(t, []string{"features/sample.feature:3", "features/sample.feature:7"}, paths)
}

func TestNewRunnerRequiresTargetSoperatorVersion(t *testing.T) {
	_, err := NewRunner(Options{KubectlContext: "dev-context"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "target Soperator version is required")
}

func TestDiscoveredNodeSetsFromLiveList(t *testing.T) {
	nodeSets := slurmv1alpha1.NodeSetList{
		Items: []slurmv1alpha1.NodeSet{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "worker-gpu"},
				Spec: slurmv1alpha1.NodeSetSpec{
					ClusterName: "soperator",
					Replicas:    2,
					GPU:         slurmv1alpha1.GPUSpec{Enabled: true},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "worker-cpu"},
				Spec: slurmv1alpha1.NodeSetSpec{
					ClusterName: "soperator",
					Replicas:    3,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "other-worker"},
				Spec: slurmv1alpha1.NodeSetSpec{
					ClusterName: "other",
					Replicas:    4,
					GPU:         slurmv1alpha1.GPUSpec{Enabled: true},
				},
			},
		},
	}

	discovered := discoveredNodeSetsFromLiveList(nodeSets, "soperator")
	require.Len(t, discovered, 2)

	assert.Equal(t, "worker-cpu", discovered[0].Name)
	assert.Equal(t, 3, discovered[0].Size)
	assert.False(t, discovered[0].HasGPU)

	assert.Equal(t, "worker-gpu", discovered[1].Name)
	assert.Equal(t, 2, discovered[1].Size)
	assert.True(t, discovered[1].HasGPU)
}

func TestDiscoveredNodeSetsFromLiveListDoesNotFilterWhenClusterNameIsEmpty(t *testing.T) {
	nodeSets := slurmv1alpha1.NodeSetList{
		Items: []slurmv1alpha1.NodeSet{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "worker-gpu"},
				Spec: slurmv1alpha1.NodeSetSpec{
					ClusterName: "soperator",
					Replicas:    2,
					GPU:         slurmv1alpha1.GPUSpec{Enabled: true},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "worker-cpu"},
				Spec: slurmv1alpha1.NodeSetSpec{
					ClusterName: "soperator",
					Replicas:    3,
				},
			},
		},
	}

	discovered := discoveredNodeSetsFromLiveList(nodeSets, "")
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

	discovered, err := workerPodsBySlurmNodeName(pods)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"worker-a": "kube-worker-a",
		"worker-b": "kube-worker-b",
	}, discovered)
}

func TestWorkerPodsBySlurmNodeNameRejectsMissingHostname(t *testing.T) {
	_, err := workerPodsBySlurmNodeName(corev1.PodList{
		Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "worker-0"}}},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "empty spec.hostname")
}

func TestWorkerPodsBySlurmNodeNameRejectsDuplicateHostname(t *testing.T) {
	_, err := workerPodsBySlurmNodeName(corev1.PodList{
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

	classifyWorkers(state)

	assert.ElementsMatch(t, []framework.WorkerRef{{Name: "worker-cpu-0"}}, state.CPUWorkers)
	assert.ElementsMatch(t, []framework.WorkerRef{{Name: "worker-gpu-0"}, {Name: "worker-gpu-1"}}, state.GPUWorkers)
	assert.ElementsMatch(t, []framework.WorkerRef{{Name: "worker-cpu-0"}}, state.WorkersByNodeSet["worker-cpu"])
	assert.ElementsMatch(t, []framework.WorkerRef{{Name: "worker-gpu-0"}, {Name: "worker-gpu-1"}}, state.WorkersByNodeSet["worker-gpu"])
}

func TestReportFormat(t *testing.T) {
	format, err := reportFormat("")
	require.NoError(t, err)
	assert.Equal(t, "pretty", format)

	dir := t.TempDir()
	format, err = reportFormat(dir)
	require.NoError(t, err)
	assert.Equal(t,
		"pretty,cucumber:"+filepath.Join(dir, "acceptance.cucumber.json")+
			",junit:"+filepath.Join(dir, "acceptance.junit.xml"),
		format,
	)
}
