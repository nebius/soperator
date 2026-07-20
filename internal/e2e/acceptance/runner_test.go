package acceptance

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Empty(t, opts.ReportDir)
}

func TestParseOptionsExplicitValues(t *testing.T) {
	opts, err := parseOptions([]string{
		"--kubectl-context", "dev-context",
		"--slurm-cluster-name", "custom",
		"--run-unstable=true",
		"--report-dir", "reports",
	})
	require.NoError(t, err)

	assert.Equal(t, "dev-context", opts.KubectlContext)
	assert.Equal(t, "custom", opts.SlurmClusterName)
	assert.True(t, opts.RunUnstableTests)
	assert.Equal(t, "reports", opts.ReportDir)
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

func TestClassifyWorkersSeparatesCPUAndGPU(t *testing.T) {
	state := &framework.ClusterState{
		Workers: []framework.WorkerPodRef{
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

	assert.ElementsMatch(t, []framework.WorkerPodRef{{Name: "worker-cpu-0"}}, state.CPUWorkers)
	assert.ElementsMatch(t, []framework.WorkerPodRef{{Name: "worker-gpu-0"}, {Name: "worker-gpu-1"}}, state.GPUWorkers)
	assert.ElementsMatch(t, []framework.WorkerPodRef{{Name: "worker-cpu-0"}}, state.WorkersByNodeSet["worker-cpu"])
	assert.ElementsMatch(t, []framework.WorkerPodRef{{Name: "worker-gpu-0"}, {Name: "worker-gpu-1"}}, state.WorkersByNodeSet["worker-gpu"])
	assert.True(t, state.HasHeterogeneousWorkers())
}

func TestTagFilterExcludesHeterogeneousScenariosWithoutCPUAndGPUWorkers(t *testing.T) {
	runner := NewRunner(&framework.ClusterState{
		GPUWorkers: []framework.WorkerPodRef{{Name: "worker-gpu-0"}},
	}, true, "dev-context", "")

	assert.Equal(t, "~@heterogeneous", runner.tagFilter())
}

func TestTagFilterAllowsHeterogeneousScenariosWithCPUAndGPUWorkers(t *testing.T) {
	runner := NewRunner(&framework.ClusterState{
		CPUWorkers: []framework.WorkerPodRef{{Name: "worker-cpu-0"}},
		GPUWorkers: []framework.WorkerPodRef{{Name: "worker-gpu-0"}},
	}, true, "dev-context", "")

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
