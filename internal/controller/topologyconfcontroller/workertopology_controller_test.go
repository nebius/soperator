package topologyconfcontroller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"nebius.ai/slurm-operator/api/v1alpha1"
)

func Test_loadTopologyConfig_Basic(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)

	r := &WorkerTopologyReconciler{
		client:              fake.NewClientBuilder().WithScheme(scheme).Build(),
		scheme:              scheme,
		reconfigureDeferral: testReconfigureDeferral,
	}
	config, err := r.loadTopologyConfig(ctx, testCluster)
	require.NoError(t, err)
	assert.Equal(t, testTopologyConfigKey, config.key)
	assert.Equal(t, testReconfigureDeferral, config.reconfigureDeferral)
	assert.Nil(t, config.jailedConfig, "nothing published yet")

	r.client = fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(testJailedConfig(v1alpha1.UpdateActionReconfigure)).Build()
	config, err = r.loadTopologyConfig(ctx, testCluster)
	require.NoError(t, err)
	require.NotNil(t, config.jailedConfig)
	assert.True(t, hasReconfigureRequest(config.jailedConfig))
}
