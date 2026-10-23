package topologyconfcontroller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/topologyconfcontroller/deferral"
	"nebius.ai/slurm-operator/internal/controller/topologyconfcontroller/topology"
)

var testReconfigureDeferral = deferral.Policy{
	Cooldown:    15 * time.Minute,
	Debounce:    time.Minute,
	DebounceCap: 5 * time.Minute,
}

var testTopologyConfigKey = client.ObjectKey{Namespace: "soperator", Name: "slurm-topology-config"}

var testCluster = &slurmv1.SlurmCluster{ObjectMeta: metav1.ObjectMeta{Name: "slurm", Namespace: "soperator", UID: "cluster-uid"}}

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	return scheme
}

// newTestTopologyConfig builds a topologyConfig over a fake client holding objects; a JailedConfig
// among them is the config's, as loadTopologyConfig would have read it.
func newTestTopologyConfig(t *testing.T, objects ...client.Object) (*topologyConfig, client.Client) {
	t.Helper()
	scheme := newTestScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.JailedConfig{}).
		WithObjects(objects...).
		Build()

	config := newTopologyConfig(fakeClient, scheme, testCluster, testReconfigureDeferral)
	for _, object := range objects {
		if _, ok := object.(*v1alpha1.JailedConfig); ok {
			config.jailedConfig = getJailedConfig(t, fakeClient)
		}
	}
	return config, fakeClient
}

func testJailedConfig(actions ...v1alpha1.UpdateAction) *v1alpha1.JailedConfig {
	return &v1alpha1.JailedConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testTopologyConfigKey.Name, Namespace: testTopologyConfigKey.Namespace},
		Spec: v1alpha1.JailedConfigSpec{
			ConfigMap:     v1alpha1.ConfigMapReference{Name: testTopologyConfigKey.Name},
			UpdateActions: actions,
		},
	}
}

func flatTopology(t *testing.T, nodes uint) *topology.MultiTopology {
	t.Helper()
	spec, err := topology.NewSpec("flat", "flat", true, nil, []string{"cpu"})
	require.NoError(t, err)
	nodeSet, err := topology.NewNodeSet("cpu", nodes, "", nil)
	require.NoError(t, err)
	topo, err := topology.NewMultiTopology([]topology.Spec{spec}, []topology.NodeSet{nodeSet})
	require.NoError(t, err)
	return topo
}

func getJailedConfig(t *testing.T, c client.Client) *v1alpha1.JailedConfig {
	t.Helper()
	jailedConfig := &v1alpha1.JailedConfig{}
	require.NoError(t, c.Get(context.Background(), testTopologyConfigKey, jailedConfig))
	return jailedConfig
}

func Test_newTopologyConfig_Basic(t *testing.T) {
	config, _ := newTestTopologyConfig(t)
	assert.Equal(t, testTopologyConfigKey, config.key)
	assert.Equal(t, testCluster, config.owner)
	assert.Nil(t, config.jailedConfig)
}

func assertOwnedByTestCluster(t *testing.T, object client.Object) {
	t.Helper()
	require.Len(t, object.GetOwnerReferences(), 1)
	owner := object.GetOwnerReferences()[0]
	assert.Equal(t, "SlurmCluster", owner.Kind)
	assert.Equal(t, testCluster.Name, owner.Name)
	assert.Equal(t, testCluster.UID, owner.UID)
	assert.True(t, *owner.Controller)
}

func Test_topologyConfig_current_Basic(t *testing.T) {
	ctx := context.Background()
	config, _ := newTestTopologyConfig(t)
	current, err := config.currentTopology(ctx)
	require.NoError(t, err)
	assert.Empty(t, current, "no JailedConfig")

	config, _ = newTestTopologyConfig(t, testJailedConfig())
	current, err = config.currentTopology(ctx)
	require.NoError(t, err)
	assert.Empty(t, current, "no ConfigMap")

	topo := flatTopology(t, 2)
	require.NoError(t, config.publishTopology(ctx, topo))
	current, err = config.currentTopology(ctx)
	require.NoError(t, err)
	assert.Equal(t, *topo, current)
}

func Test_topologyConfig_publish_Basic(t *testing.T) {
	ctx := context.Background()
	config, c := newTestTopologyConfig(t)

	require.NoError(t, config.publishTopology(ctx, flatTopology(t, 2)))
	configMap := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, testTopologyConfigKey, configMap))
	content := configMap.Data[consts.ConfigMapKeyTopologyYAML]
	assert.True(t, strings.HasPrefix(content, "# Managed by Soperator."), content)
	assert.Contains(t, content, "topology: flat")
	assert.Len(t, configMap.Data, 1)
	assertOwnedByTestCluster(t, configMap)

	configMap.Data["stale"] = "x"
	require.NoError(t, c.Update(ctx, configMap))
	require.NoError(t, config.publishTopology(ctx, flatTopology(t, 3)))
	require.NoError(t, c.Get(ctx, testTopologyConfigKey, configMap))
	assert.Len(t, configMap.Data, 1, "the ConfigMap is owned wholesale")
}

func Test_topologyConfig_requestReconfigure_Basic(t *testing.T) {
	ctx := context.Background()
	config, c := newTestTopologyConfig(t)

	require.NoError(t, config.requestReconfigure(ctx, logr.Discard()))
	created := getJailedConfig(t, c)
	assert.Equal(t, []v1alpha1.UpdateAction{v1alpha1.UpdateActionReconfigure}, created.Spec.UpdateActions)
	assert.Equal(t, testTopologyConfigKey.Name, created.Spec.ConfigMap.Name)
	assert.Equal(t, "/etc/slurm/topology.yaml", created.Spec.Items[0].Path)
	assert.Equal(t, "slurm", created.Labels[consts.LabelInstanceKey])
	assert.False(t, timeAnnotation(created, annotationLastReconfigure).IsZero(), "the anchor is stamped on create")
	assertOwnedByTestCluster(t, created)
	require.NotNil(t, config.jailedConfig)
	assert.Equal(t, created.ResourceVersion, config.jailedConfig.ResourceVersion, "the written object is remembered")

	created.Status.AppliedHash = "h1"
	require.NoError(t, c.Status().Update(ctx, created))
	require.NoError(t, config.requestReconfigure(ctx, logr.Discard()))
	updated := getJailedConfig(t, c)
	assert.Equal(t, "h1", updated.Annotations[annotationAppliedHash], "the applied hash is pinned")
	assert.Equal(t, []v1alpha1.UpdateAction{v1alpha1.UpdateActionReconfigure}, updated.Spec.UpdateActions)
}

func Test_topologyConfig_withdrawConfirmedReconfigure_Basic(t *testing.T) {
	ctx := context.Background()

	t.Run("no request", func(t *testing.T) {
		config, c := newTestTopologyConfig(t, testJailedConfig())
		before := getJailedConfig(t, c).ResourceVersion
		require.NoError(t, config.withdrawConfirmedReconfigure(ctx, logr.Discard()))
		assert.Equal(t, before, getJailedConfig(t, c).ResourceVersion, "nothing written")
	})

	t.Run("unconfirmed request", func(t *testing.T) {
		config, c := newTestTopologyConfig(t, testJailedConfig(v1alpha1.UpdateActionReconfigure))
		before := getJailedConfig(t, c).ResourceVersion
		require.NoError(t, config.withdrawConfirmedReconfigure(ctx, logr.Discard()))
		assert.Equal(t, before, getJailedConfig(t, c).ResourceVersion, "nothing written")
	})

	t.Run("confirmed request", func(t *testing.T) {
		jailedConfig := testJailedConfig(v1alpha1.UpdateActionReconfigure)
		jailedConfig.Generation = 3
		config, c := newTestTopologyConfig(t, jailedConfig)
		jailedConfig = getJailedConfig(t, c)
		meta.SetStatusCondition(&jailedConfig.Status.Conditions, metav1.Condition{
			Type: string(v1alpha1.ReconfigurePerformed), Status: metav1.ConditionTrue, Reason: "Success", ObservedGeneration: 3,
		})
		require.NoError(t, c.Status().Update(ctx, jailedConfig))
		config.jailedConfig = getJailedConfig(t, c)

		require.NoError(t, config.withdrawConfirmedReconfigure(ctx, logr.Discard()))
		withdrawn := getJailedConfig(t, c)
		assert.Empty(t, withdrawn.Spec.UpdateActions)
		assert.False(t, timeAnnotation(withdrawn, annotationLastReconfigure).IsZero(), "the anchor is stamped on withdrawal")
		assert.Empty(t, config.jailedConfig.Spec.UpdateActions, "the remembered object is the written one")
	})
}

func Test_topologyConfig_update_KeepsUnconfirmedRequest(t *testing.T) {
	ctx := context.Background()
	config, c := newTestTopologyConfig(t, testJailedConfig(v1alpha1.UpdateActionReconfigure))
	require.NoError(t, config.update(ctx, logr.Discard(), false))
	assert.Equal(t, []v1alpha1.UpdateAction{v1alpha1.UpdateActionReconfigure}, getJailedConfig(t, c).Spec.UpdateActions)
}

func Test_reconfigureConfirmed_Basic(t *testing.T) {
	jailedConfig := func(generation, observed int64, condition metav1.ConditionStatus, applied, pinned string) *v1alpha1.JailedConfig {
		jc := testJailedConfig(v1alpha1.UpdateActionReconfigure)
		jc.Generation = generation
		jc.Annotations = map[string]string{annotationAppliedHash: pinned}
		jc.Status.AppliedHash = applied
		if condition != "" {
			jc.Status.Conditions = []metav1.Condition{{
				Type: string(v1alpha1.ReconfigurePerformed), Status: condition, ObservedGeneration: observed,
			}}
		}
		return jc
	}
	assert.False(t, reconfigureConfirmed(jailedConfig(2, 2, "", "h2", "h1")), "no condition")
	assert.False(t, reconfigureConfirmed(jailedConfig(2, 2, metav1.ConditionFalse, "h2", "h1")), "condition false")
	assert.True(t, reconfigureConfirmed(jailedConfig(2, 2, metav1.ConditionTrue, "h1", "h1")), "current generation")
	assert.True(t, reconfigureConfirmed(jailedConfig(2, 1, metav1.ConditionTrue, "h2", "h1")), "hash moved past the pin")
	assert.False(t, reconfigureConfirmed(jailedConfig(2, 1, metav1.ConditionTrue, "h1", "h1")), "earlier generation, same hash")
}

func Test_topologyConfig_deferReconfigure_Basic(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	config, c := newTestTopologyConfig(t, testJailedConfig())

	wait, err := config.deferReconfigure(ctx, t0)
	require.NoError(t, err)
	assert.Equal(t, testReconfigureDeferral.Debounce, wait)
	booked := getJailedConfig(t, c)
	assert.Equal(t, t0, timeAnnotation(booked, annotationDeferredSince))
	assert.Equal(t, t0, timeAnnotation(booked, annotationDeferredLastChange))

	wait, err = config.deferReconfigure(ctx, t0.Add(30*time.Second))
	require.NoError(t, err)
	assert.Equal(t, testReconfigureDeferral.Debounce, wait, "a change restarts the debounce")
	booked = getJailedConfig(t, c)
	assert.Equal(t, t0, timeAnnotation(booked, annotationDeferredSince), "the booking keeps its start")
	assert.Equal(t, t0.Add(30*time.Second), timeAnnotation(booked, annotationDeferredLastChange))

	wait, err = config.deferReconfigure(ctx, t0.Add(testReconfigureDeferral.DebounceCap))
	require.NoError(t, err)
	assert.Zero(t, wait, "the cap is reached")
}

func Test_topologyConfig_deferReconfigure_Cooldown(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	jailedConfig := testJailedConfig()
	jailedConfig.Annotations = map[string]string{annotationLastReconfigure: t0.Format(time.RFC3339)}
	config, _ := newTestTopologyConfig(t, jailedConfig)

	wait, err := config.deferReconfigure(ctx, t0.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, testReconfigureDeferral.Cooldown-time.Minute, wait)
}

func Test_topologyConfig_deferReconfigure_NothingPublished(t *testing.T) {
	config, _ := newTestTopologyConfig(t)
	wait, err := config.deferReconfigure(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Zero(t, wait)
}

func Test_topologyConfig_pendingDeferredReconfigure_Basic(t *testing.T) {
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	withAnnotations := func(annotations map[string]string) *topologyConfig {
		jailedConfig := testJailedConfig()
		jailedConfig.Annotations = annotations
		config, _ := newTestTopologyConfig(t, jailedConfig)
		return config
	}
	booking := map[string]string{
		annotationDeferredSince:      t0.Format(time.RFC3339),
		annotationDeferredLastChange: t0.Add(30 * time.Second).Format(time.RFC3339),
	}

	config, _ := newTestTopologyConfig(t)
	pending, _ := config.pendingDeferredReconfigure(t0)
	assert.False(t, pending, "nothing published")

	pending, _ = withAnnotations(nil).pendingDeferredReconfigure(t0)
	assert.False(t, pending, "nothing booked")

	pending, wait := withAnnotations(booking).pendingDeferredReconfigure(t0.Add(time.Minute))
	assert.True(t, pending)
	assert.Equal(t, 30*time.Second, wait)

	pending, wait = withAnnotations(booking).pendingDeferredReconfigure(t0.Add(2 * time.Minute))
	assert.True(t, pending)
	assert.Zero(t, wait, "due")

	settled := map[string]string{annotationLastReconfigure: t0.Add(time.Minute).Format(time.RFC3339)}
	for k, v := range booking {
		settled[k] = v
	}
	pending, _ = withAnnotations(settled).pendingDeferredReconfigure(t0.Add(2 * time.Minute))
	assert.False(t, pending, "settled by a later reconfigure")
}

// Test_topologyConfig_currentTopology_RoundTrip pins that a published topology reads back as
// identical: a difference would publish on every pass.
func Test_topologyConfig_currentTopology_RoundTrip(t *testing.T) {
	ctx := context.Background()
	path := func(names ...string) topology.NetworkPath {
		var segments []topology.NetworkPathSegment
		for tier, name := range names {
			segments = append(segments, topology.NetworkPathSegment{Tier: uint(tier), Name: name})
		}
		p, err := topology.NewNetworkPath(segments...)
		require.NoError(t, err)
		return p
	}
	spec := func(name, kind string, clusterDefault bool, blockSizes []int, nodeSets ...string) topology.Spec {
		s, err := topology.NewSpec(name, kind, clusterDefault, blockSizes, nodeSets)
		require.NoError(t, err)
		return s
	}
	nodeSet := func(name string, size uint, fabric string, paths map[string]topology.NetworkPath) topology.NodeSet {
		ns, err := topology.NewNodeSet(name, size, fabric, paths)
		require.NoError(t, err)
		return ns
	}

	tests := []struct {
		name     string
		specs    []topology.Spec
		nodeSets []topology.NodeSet
	}{
		{
			name: "flat, tree with a down node and block topologies",
			specs: []topology.Spec{
				spec("flat", "flat", false, nil, "cpu"),
				spec("tree", "tree", true, nil, "worker", "worker-rack0"),
				spec("block", "block", false, []int{2}, "worker-rack0"),
			},
			nodeSets: []topology.NodeSet{
				nodeSet("cpu", 2, "", nil),
				nodeSet("worker", 4, "gpucluster-2a9e", map[string]topology.NetworkPath{
					"worker-0": path("t0-77c1e0", "t1-4b9f3a"),
					"worker-1": path("t0-77c1e0", "t1-4b9f3a"),
					"worker-2": path("t0-b4d2f0", "t1-4b9f3a"),
				}),
				nodeSet("worker-rack0", 2, "gpucluster-1f0c", map[string]topology.NetworkPath{
					"worker-rack0-0": path("t0-a3f0b9", "t1-1c7d4e"),
					"worker-rack0-1": path("t0-a3f0b9", "t1-1c7d4e"),
				}),
			},
		},
		{
			name: "tree and block placeholders",
			specs: []topology.Spec{
				spec("tree", "tree", true, nil, "worker-rack0"),
				spec("block", "block", false, []int{2}, "worker-rack0"),
			},
			nodeSets: []topology.NodeSet{nodeSet("worker-rack0", 0, "gpucluster-1f0c", nil)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			topo, err := topology.NewMultiTopology(tt.specs, tt.nodeSets)
			require.NoError(t, err)

			config, _ := newTestTopologyConfig(t, testJailedConfig())
			require.NoError(t, config.publishTopology(ctx, topo))
			current, err := config.currentTopology(ctx)
			require.NoError(t, err)
			assert.Equal(t, topology.ChangeIdentical, topo.ChangeFrom(&current))
		})
	}
}
