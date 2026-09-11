package worker_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/values"
)

func TestWorkerDisruptionBudgetProtectsPodsUntilOperationReady(t *testing.T) {
	nodeSet := &values.SlurmNodeSet{
		Name: "gpu", ParentalCluster: client.ObjectKey{Namespace: "default", Name: "cluster"},
		StatefulSet: values.StatefulSet{Name: "workers"},
	}
	pdb := worker.RenderPodDisruptionBudget(nodeSet)
	assert.Equal(t, "workers", pdb.Name)
	assert.Equal(t, "default", pdb.Namespace)
	require.NotNil(t, pdb.Spec.MaxUnavailable)
	assert.Zero(t, pdb.Spec.MaxUnavailable.IntVal)
	assert.Equal(t, policyv1.IfHealthyBudget, *pdb.Spec.UnhealthyPodEvictionPolicy)
	selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
	require.NoError(t, err)
	assert.True(t, selector.Matches(labels.Set(pdb.Spec.Selector.MatchLabels)), "new pods without operation labels stay protected")
	for _, phase := range []string{"", consts.LabelSoperatorWorkerOperationPhaseStopping, "unknown"} {
		t.Run("phase="+phase, func(t *testing.T) {
			podLabels := labels.Merge(pdb.Spec.Selector.MatchLabels, labels.Set{consts.LabelSoperatorWorkerOperationPhase: phase})
			assert.True(t, selector.Matches(podLabels))
			podLabels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseReady
			assert.False(t, selector.Matches(podLabels))
		})
	}
	for key, value := range map[string]string{consts.LabelNodeSetKey: "other-nodeset", consts.LabelInstanceKey: "other-cluster"} {
		podLabels := labels.Merge(pdb.Spec.Selector.MatchLabels, labels.Set{key: value})
		assert.False(t, selector.Matches(podLabels))
	}
}
