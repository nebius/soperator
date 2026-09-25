package topologyconfcontroller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/utils/resourcegetter"
)

func TestDeferrableTopologyChange(t *testing.T) {
	render := func(entries ...topologyYAMLEntry) string {
		rendered, err := renderTopologyYAML(entries)
		require.NoError(t, err)
		return rendered
	}
	blocks := func(sizes []int, names ...string) topologyYAMLEntry {
		entry := topologyYAMLEntry{Topology: "blocks", Block: &blockTopologyYAML{BlockSizes: sizes}}
		for _, name := range names {
			entry.Block.Blocks = append(entry.Block.Blocks, blockYAML{Block: name, Nodes: name + "-0"})
		}
		return entry
	}
	tree := topologyYAMLEntry{Topology: "tree", Tree: &treeTopologyYAML{Switches: []switchYAML{{Switch: "root"}}}}
	grownTree := topologyYAMLEntry{Topology: "tree", Tree: &treeTopologyYAML{Switches: []switchYAML{
		{Switch: "root", Children: "leaf"}, {Switch: "leaf", Nodes: "cpu-0"},
	}}}
	published := render(blocks([]int{2}, "a", "b", "c"), tree)

	for name, tc := range map[string]struct {
		desired string
		want    bool
	}{
		"order changed":            {render(blocks([]int{2}, "c", "a", "b"), tree), true},
		"block removed":            {render(blocks([]int{2}, "a", "c"), tree), true},
		"block removed, reordered": {render(blocks([]int{2}, "c", "a"), tree), true},
		"block added":              {render(blocks([]int{2}, "a", "b", "c", "d"), tree), false},
		"block replaced":           {render(blocks([]int{2}, "a", "b", "d"), tree), false},
		"block sizes changed":      {render(blocks([]int{4}, "c", "a", "b"), tree), false},
		"topology added": {render(blocks([]int{2}, "c", "a", "b"), tree,
			topologyYAMLEntry{Topology: "flat", Flat: true}), false},
		"cluster default changed": {render(blocks([]int{2}, "c", "a", "b"),
			topologyYAMLEntry{Topology: "tree", ClusterDefault: true, Tree: tree.Tree}), false},
		"topologies reordered": {render(tree, blocks([]int{2}, "a", "b", "c")), false},
		"only node lists changed": {render(topologyYAMLEntry{Topology: "blocks", Block: &blockTopologyYAML{
			BlockSizes: []int{2},
			Blocks:     []blockYAML{{Block: "a", Nodes: "a-[0-1]"}, {Block: "b"}, {Block: "c", Nodes: "c-0"}},
		}}, tree), false},
		"unchanged":                {published, false},
		"tree changed":             {render(blocks([]int{2}, "a", "b", "c"), grownTree), false},
		"tree changed, block gone": {render(blocks([]int{2}, "a", "c"), grownTree), false},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, deferrableTopologyChange(renderManagedTopologyConfig(published), tc.desired))
		})
	}

	t.Run("placeholder is never deferred", func(t *testing.T) {
		placeholder := render(emptyTopologyEntry(slurmv1.NamedTopology{
			Name: "blocks", Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeBlock},
		}))
		assert.False(t, deferrableTopologyChange(placeholder, render(blocks(nil, "a"))))
	})

	t.Run("unparsable published config is never deferred", func(t *testing.T) {
		assert.False(t, deferrableTopologyChange("SwitchName=root", published))
	})
}

func TestReconfigureLimiter(t *testing.T) {
	first := types.NamespacedName{Namespace: "soperator", Name: "first"}
	second := types.NamespacedName{Namespace: "soperator", Name: "second"}
	now := time.Now()
	spend := func(limiter *reconfigureLimiter, key types.NamespacedName, at time.Time) bool {
		if !limiter.hasToken(key, at) {
			return false
		}
		limiter.take(key, at)
		return true
	}

	t.Run("zero interval disables the limit", func(t *testing.T) {
		var limiter reconfigureLimiter
		for range 10 {
			assert.True(t, spend(&limiter, first, now))
		}
	})

	t.Run("burst, then one per interval", func(t *testing.T) {
		limiter := reconfigureLimiter{interval: 11 * time.Minute, burst: 2}
		assert.True(t, spend(&limiter, first, now))
		assert.True(t, spend(&limiter, first, now))
		assert.False(t, spend(&limiter, first, now))
		assert.False(t, spend(&limiter, first, now.Add(10*time.Minute)))
		assert.True(t, spend(&limiter, first, now.Add(11*time.Minute)))
		assert.False(t, spend(&limiter, first, now.Add(11*time.Minute)))
	})

	t.Run("checking does not spend a token", func(t *testing.T) {
		limiter := reconfigureLimiter{interval: time.Hour, burst: 1}
		for range 3 {
			assert.True(t, limiter.hasToken(first, now))
		}
		limiter.take(first, now)
		assert.False(t, limiter.hasToken(first, now))
	})

	t.Run("clusters do not share a bucket", func(t *testing.T) {
		limiter := reconfigureLimiter{interval: time.Hour, burst: 1}
		assert.True(t, spend(&limiter, first, now))
		assert.False(t, spend(&limiter, first, now))
		assert.True(t, spend(&limiter, second, now))
	})

	t.Run("forget refills the bucket", func(t *testing.T) {
		limiter := reconfigureLimiter{interval: time.Hour, burst: 1}
		assert.True(t, spend(&limiter, first, now))
		limiter.forget(first)
		assert.True(t, spend(&limiter, first, now))
	})
}

// TestReconcileDefersBlockOrderChange pins that a change which only reorders blocks waits for a
// token without touching the published ConfigMap, that a failed publish does not spend the token, and
// that a new block is published at once.
func TestReconcileDefersBlockOrderChange(t *testing.T) {
	const namespace = "soperator"
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(slurmv1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	cluster := clusterWithTopologies(slurmv1.NamedTopology{
		Name: "blocks", Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeBlock},
		NodeSetRefs: []string{consts.SlurmTopologyNodeSetRefAll},
	})
	cluster.Namespace, cluster.Name = namespace, "slurm"
	key := client.ObjectKeyFromObject(cluster)

	rack := func(name, node string) []client.Object {
		nodeSet := gpuNodeSet("rack-"+name, 1, "")
		nodeSet.Namespace = namespace
		nodeSet.Spec.ClusterName = cluster.Name
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeSet.Name + "-0", Namespace: namespace,
				Labels: map[string]string{consts.LabelNodeSetKey: nodeSet.Name},
			},
			Spec: corev1.PodSpec{NodeName: node},
		}
		return []client.Object{&nodeSet, pod}
	}
	nodeLabels := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: consts.ConfigMapNameTopologyNodeLabels, Namespace: namespace},
		Data: map[string]string{
			"a": `{"tier-0":"block-a","tier-1":"leaf-a"}`,
			"b": `{"tier-0":"block-b","tier-1":"leaf-b"}`,
		},
	}
	objects := append([]client.Object{cluster, nodeLabels}, rack("a", "a")...)
	objects = append(objects, rack("b", "b")...)
	failConfigMapUpdate := false
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*corev1.ConfigMap); ok && failConfigMapUpdate && obj.GetName() != nodeLabels.Name {
					return errors.New("injected ConfigMap update failure")
				}
				return c.Update(ctx, obj, opts...)
			},
		}).Build()
	r := NewWorkerTopologyReconciler(fakeClient, scheme, namespace, events.NewFakeRecorder(100), time.Hour, 1)

	topologyConfigKey := types.NamespacedName{
		Namespace: namespace,
		Name: resourcegetter.BuildPrefixedName(
			resourcegetter.ResolvePodNamePrefix(cluster.Name, cluster.Spec.PodNamePrefix),
			consts.ConfigMapNameTopologyConfig,
		),
	}
	reconcile := func() (published, structure string) {
		t.Helper()
		_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
		require.NoError(t, err)
		var configMap corev1.ConfigMap
		require.NoError(t, fakeClient.Get(t.Context(), topologyConfigKey, &configMap))
		var jailedConfig v1alpha1.JailedConfig
		require.NoError(t, fakeClient.Get(t.Context(), topologyConfigKey, &jailedConfig))
		return configMap.Data[consts.ConfigMapKeyTopologyYAML], jailedConfig.Annotations[consts.AnnotationTopologyStructure]
	}
	setLeaves := func(leafA, leafB string) {
		t.Helper()
		nodeLabels.Data = map[string]string{
			"a": `{"tier-0":"block-a","tier-1":"` + leafA + `"}`,
			"b": `{"tier-0":"block-b","tier-1":"` + leafB + `"}`,
		}
		require.NoError(t, fakeClient.Update(t.Context(), nodeLabels))
	}

	initial, _ := reconcile()
	assert.Less(t, strings.Index(initial, "block-a"), strings.Index(initial, "block-b"), "new blocks are published at once")

	setLeaves("leaf-z", "leaf-b")
	failConfigMapUpdate = true
	_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
	require.Error(t, err)
	failConfigMapUpdate = false
	reordered, reorderedStructure := reconcile()
	assert.Less(t, strings.Index(reordered, "block-b"), strings.Index(reordered, "block-a"), "the burst token survives a failed publish and publishes the first reorder")

	setLeaves("leaf-a", "leaf-b")
	deferred, deferredStructure := reconcile()
	assert.Equal(t, reordered, deferred, "a reorder without a token leaves the ConfigMap alone")
	assert.Equal(t, reorderedStructure, deferredStructure, "a reorder without a token raises no request")

	require.NoError(t, fakeClient.Create(t.Context(), rack("c", "c")[0]))
	require.NoError(t, fakeClient.Create(t.Context(), rack("c", "c")[1]))
	nodeLabels.Data["c"] = `{"tier-0":"block-c","tier-1":"leaf-c"}`
	require.NoError(t, fakeClient.Update(t.Context(), nodeLabels))
	grown, _ := reconcile()
	assert.Contains(t, grown, "block-c", "a new block does not wait for a token")
	assert.Less(t, strings.Index(grown, "block-a"), strings.Index(grown, "block-b"), "the deferred reorder rides along")
}
