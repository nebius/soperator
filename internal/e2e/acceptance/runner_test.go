package acceptance

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

func TestParseOptionsDefaults(t *testing.T) {
	opts, err := parseOptions([]string{"--kubectl-context", "dev-context"})
	require.NoError(t, err)

	assert.Equal(t, "dev-context", opts.KubectlContext)
	assert.Equal(t, "soperator", opts.SlurmClusterName)
	assert.False(t, opts.RunUnstableTests)
	assert.Empty(t, opts.ScenarioPaths)
	assert.Empty(t, opts.ReportDir)
}

func TestParseOptionsExplicitValues(t *testing.T) {
	opts, err := parseOptions([]string{
		"--kubectl-context", "dev-context",
		"--slurm-cluster-name", "custom",
		"--run-unstable=true",
		"--scenario", "features/internal_ssh.feature:2",
		"--scenario=features/topology.feature:3",
		"--report-dir", "reports",
	})
	require.NoError(t, err)

	assert.Equal(t, "dev-context", opts.KubectlContext)
	assert.Equal(t, "custom", opts.SlurmClusterName)
	assert.True(t, opts.RunUnstableTests)
	assert.Equal(t, []string{"features/internal_ssh.feature:2", "features/topology.feature:3"}, opts.ScenarioPaths)
	assert.Equal(t, "reports", opts.ReportDir)
}

func TestRunnerTagFilter(t *testing.T) {
	heterogeneousState := &framework.ClusterState{
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
			state: heterogeneousState,
			want:  "~@unstable",
		},
		{
			name:             "run unstable has no tag filter when CPU and GPU workers exist",
			state:            heterogeneousState,
			runUnstableTests: true,
			want:             "",
		},
		{
			name:  "without GPU workers also excludes GPU",
			state: noGPUState,
			want:  "~@unstable && ~@gpu && ~@cpu && ~@heterogeneous",
		},
		{
			name:             "without CPU workers excludes CPU and heterogeneous",
			state:            gpuOnlyState,
			runUnstableTests: true,
			want:             "~@cpu && ~@heterogeneous",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := NewRunner(tt.state, tt.runUnstableTests, nil, "", "")
			assert.Equal(t, tt.want, runner.tagFilter())
		})
	}
}

func TestRunnerFeaturePaths(t *testing.T) {
	runner := NewRunner(&framework.ClusterState{}, false, nil, "", "")
	assert.Equal(t, featurePaths(), runner.featurePaths())

	scenarios := []string{"features/internal_ssh.feature:2", "features/topology.feature:3"}
	runner = NewRunner(&framework.ClusterState{}, false, scenarios, "", "")
	assert.Equal(t, scenarios, runner.featurePaths())
}

func TestParseOptionsRequiresKubectlContext(t *testing.T) {
	_, err := parseOptions(nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "--kubectl-context is required")
}

func TestParseOptionsRejectsExtraArgs(t *testing.T) {
	_, err := parseOptions([]string{"--kubectl-context", "dev-context", "extra"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "unexpected acceptance arguments")
}

func TestParseOptionsRejectsEmptyScenario(t *testing.T) {
	_, err := parseOptions([]string{"--kubectl-context", "dev-context", "--scenario", " "})
	require.Error(t, err)
	assert.ErrorContains(t, err, "--scenario value cannot be empty")
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
	assert.True(t, state.IsHeterogeneousCluster())
}

func TestTagFilterExcludesHeterogeneousScenariosWithoutCPUAndGPUWorkers(t *testing.T) {
	runner := NewRunner(&framework.ClusterState{
		GPUWorkers: []framework.WorkerRef{{Name: "worker-gpu-0"}},
	}, true, nil, "dev-context", "")

	assert.Equal(t, "~@cpu && ~@heterogeneous", runner.tagFilter())
}

func TestTagFilterAllowsHeterogeneousScenariosWithCPUAndGPUWorkers(t *testing.T) {
	runner := NewRunner(&framework.ClusterState{
		CPUWorkers: []framework.WorkerRef{{Name: "worker-cpu-0"}},
		GPUWorkers: []framework.WorkerRef{{Name: "worker-gpu-0"}},
	}, true, nil, "dev-context", "")

	assert.Empty(t, runner.tagFilter())
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
