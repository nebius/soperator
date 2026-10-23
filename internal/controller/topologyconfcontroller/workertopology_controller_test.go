package topologyconfcontroller

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/topologyconfcontroller/topology"
)

const testNamespace = "soperator"

// newTestReconciler builds a reconciler over a fake client holding objects, recording events into
// the returned recorder.
func newTestReconciler(t *testing.T, objects ...client.Object) (*WorkerTopologyReconciler, client.Client, *events.FakeRecorder) {
	t.Helper()
	scheme := newTestScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.JailedConfig{}).
		WithObjects(objects...).
		Build()
	recorder := events.NewFakeRecorder(20)
	r := &WorkerTopologyReconciler{
		client:              fakeClient,
		scheme:              scheme,
		namespace:           testNamespace,
		truncEventRecorder:  NewTruncateEventRecorder(recorder),
		reconfigureDeferral: testReconfigureDeferral,
	}
	return r, fakeClient, recorder
}

func testSlurmCluster(topologies ...slurmv1.NamedTopology) *slurmv1.SlurmCluster {
	cluster := testCluster.DeepCopy()
	if len(topologies) > 0 {
		cluster.Spec.Topology = &slurmv1.Topology{Topologies: topologies}
	}
	return cluster
}

func testNodeSet(name string, replicas int32, gpu bool, fabric string) *v1alpha1.NodeSet {
	return &v1alpha1.NodeSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: v1alpha1.NodeSetSpec{
			ClusterName: testCluster.Name,
			Replicas:    replicas,
			GPU:         v1alpha1.GPUSpec{Enabled: gpu},
			Topology:    v1alpha1.NodeSetTopology{Fabric: fabric},
		},
	}
}

func testPod(name, nodeSet, node string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, Labels: map[string]string{consts.LabelNodeSetKey: nodeSet}},
		Spec:       corev1.PodSpec{NodeName: node},
	}
}

func testLabelsConfigMap(t *testing.T, labelsByNode map[string]map[string]string) *corev1.ConfigMap {
	t.Helper()
	data := make(map[string]string, len(labelsByNode))
	for node, labels := range labelsByNode {
		encoded, err := json.Marshal(labels)
		require.NoError(t, err)
		data[node] = string(encoded)
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: consts.ConfigMapNameTopologyNodeLabels, Namespace: testNamespace},
		Data:       data,
	}
}

func drainEvents(recorder *events.FakeRecorder) []string {
	var recorded []string
	for len(recorder.Events) > 0 {
		recorded = append(recorded, <-recorder.Events)
	}
	return recorded
}

func reconcileTestCluster(t *testing.T, r *WorkerTopologyReconciler) ctrl.Result {
	t.Helper()
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: testCluster.Name, Namespace: testNamespace},
	})
	require.NoError(t, err)
	return result
}

func getTopologyConfigMap(t *testing.T, c client.Client) *corev1.ConfigMap {
	t.Helper()
	configMap := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), testTopologyConfigKey, configMap))
	return configMap
}

// publishedSwitchNodes maps the switches of the published tree topology to their node lists.
func publishedSwitchNodes(t *testing.T, c client.Client, topologyName string) map[string]string {
	t.Helper()
	var published topology.MultiTopology
	require.NoError(t, yaml.Unmarshal([]byte(getTopologyConfigMap(t, c).Data[consts.ConfigMapKeyTopologyYAML]), &published))
	for _, topo := range published {
		if topo.Name != topologyName {
			continue
		}
		require.NotNil(t, topo.Tree, "topology %q is not a tree", topologyName)
		nodes := make(map[string]string, len(topo.Tree.Switches))
		for _, sw := range topo.Tree.Switches {
			nodes[sw.Name] = string(sw.Nodes)
		}
		return nodes
	}
	t.Fatalf("topology %q is not published", topologyName)
	return nil
}

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

func Test_listedNodeSetNames_Basic(t *testing.T) {
	nodeSet := func(name string, gpu bool, aggregates ...string) v1alpha1.NodeSet {
		return v1alpha1.NodeSet{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       v1alpha1.NodeSetSpec{GPU: v1alpha1.GPUSpec{Enabled: gpu}, AggregateSlurmNodeSets: aggregates},
		}
	}
	nodeSets := []v1alpha1.NodeSet{
		nodeSet("worker-rack0", true, "worker"),
		nodeSet("worker-rack1", true, "worker"),
		nodeSet("hgx", true, "gpu-all"),
		nodeSet("cpu", false),
	}

	tests := []struct {
		name     string
		topoType string
		refs     []string
		want     []string
	}{
		{name: "NodeSet name", topoType: consts.SlurmTopologyTypeTree, refs: []string{"hgx"}, want: []string{"hgx"}},
		{name: "aggregate covers every NodeSet listing it", topoType: consts.SlurmTopologyTypeBlock, refs: []string{"worker"}, want: []string{"worker-rack0", "worker-rack1"}},
		{name: "names and aggregates mix", topoType: consts.SlurmTopologyTypeTree, refs: []string{"worker", "hgx"}, want: []string{"worker-rack0", "worker-rack1", "hgx"}},
		{name: "ALL lists GPU NodeSets in a tree", topoType: consts.SlurmTopologyTypeTree, refs: []string{consts.SlurmTopologyNodeSetRefAll}, want: []string{"worker-rack0", "worker-rack1", "hgx"}},
		{name: "no refs lists every NodeSet in a flat topology", topoType: consts.SlurmTopologyTypeFlat, want: []string{"worker-rack0", "worker-rack1", "hgx", "cpu"}},
		{name: "a CPU-only NodeSet is left out of a tree", topoType: consts.SlurmTopologyTypeTree, refs: []string{"cpu"}},
		{name: "unknown ref", topoType: consts.SlurmTopologyTypeTree, refs: []string{"missing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, listedNodeSetNames(nodeSets, tt.topoType, tt.refs))
		})
	}

	t.Run("an aggregate named like a NodeSet refers to that NodeSet only", func(t *testing.T) {
		shadowing := append(nodeSets, nodeSet("worker", true))
		assert.Equal(t, []string{"worker"}, listedNodeSetNames(shadowing, consts.SlurmTopologyTypeBlock, []string{"worker"}))
	})
}

func Test_podOrdinal_Basic(t *testing.T) {
	tests := []struct {
		pod  string
		want int32
		ok   bool
	}{
		{pod: "worker-0", want: 0, ok: true},
		{pod: "worker-17", want: 17, ok: true},
		{pod: "worker-rack0-3", want: 0, ok: false},
		{pod: "worker-", ok: false},
		{pod: "worker-01", ok: false},
		{pod: "worker--1", ok: false},
		{pod: "login-0", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.pod, func(t *testing.T) {
			ordinal, ok := podOrdinal(tt.pod, "worker")
			assert.Equal(t, tt.ok, ok)
			if ok {
				assert.Equal(t, tt.want, ordinal)
			}
		})
	}
}

func Test_summarizeSamples_Basic(t *testing.T) {
	assert.Equal(t, "", summarizeSamples(nil))
	assert.Equal(t, "a; b", summarizeSamples([]string{"b", "a"}))
	assert.Equal(t, "a; b; c; d; e; and 2 more", summarizeSamples([]string{"g", "f", "e", "d", "c", "b", "a"}))
}

func Test_newTopoSpecs_RecordsInvalidSpec(t *testing.T) {
	recorder := events.NewFakeRecorder(10)
	r := &WorkerTopologyReconciler{truncEventRecorder: NewTruncateEventRecorder(recorder)}
	cluster := &slurmv1.SlurmCluster{Spec: slurmv1.SlurmClusterSpec{Topology: &slurmv1.Topology{Topologies: []slurmv1.NamedTopology{
		{Name: "tree", Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeTree}, ClusterDefault: new(true)},
		{Name: "ring", Topo: slurmv1.TopologyPlugin{Type: "ring"}},
	}}}}

	specs := r.newTopoSpecs(logr.Discard(), cluster, nil)
	require.Len(t, specs, 1)
	assert.Equal(t, "tree", specs[0].Name())

	require.Len(t, recorder.Events, 1)
	event := <-recorder.Events
	assert.Contains(t, event, reasonInvalidTopologySpec)
	assert.Contains(t, event, `"ring"`)
}

func Test_findSlurmClustersInNamespace_Basic(t *testing.T) {
	other := testSlurmCluster()
	other.Name = "other"
	elsewhere := testSlurmCluster()
	elsewhere.Namespace = "elsewhere"
	r, _, _ := newTestReconciler(t, testSlurmCluster(), other, elsewhere)

	requests := r.findSlurmClustersInNamespace(context.Background(), testNodeSet("worker", 1, true, ""))
	assert.ElementsMatch(t, []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: "slurm", Namespace: testNamespace}},
		{NamespacedName: types.NamespacedName{Name: "other", Namespace: testNamespace}},
	}, requests)
}

func Test_loadTopologyLabels_Basic(t *testing.T) {
	ctx := context.Background()
	labels := map[string]map[string]string{
		"node-a": {"tier-0": "t0-77c1e0", "tier-1": "t1-4b9f3a"},
		"node-b": {"tier-0": "t0-b4d2f0"},
	}
	r, _, _ := newTestReconciler(t, testLabelsConfigMap(t, labels))
	loaded, err := r.loadTopologyLabels(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]topologyLabels{
		"node-a": {"tier-0": "t0-77c1e0", "tier-1": "t1-4b9f3a"},
		"node-b": {"tier-0": "t0-b4d2f0"},
	}, loaded)

	r, _, _ = newTestReconciler(t)
	_, err = r.loadTopologyLabels(ctx)
	assert.ErrorContains(t, err, "get node topology labels config map")

	broken := testLabelsConfigMap(t, nil)
	broken.Data = map[string]string{"node-a": "not json"}
	r, _, _ = newTestReconciler(t, broken)
	_, err = r.loadTopologyLabels(ctx)
	assert.ErrorContains(t, err, `parse topology labels for node "node-a"`)
}

func Test_parseTopologyLabels_Basic(t *testing.T) {
	path, err := parseTopologyLabels(topologyLabels{"tier-1": "t1-4b9f3a", "tier-0": "t0-77c1e0"})
	require.NoError(t, err)
	assert.Equal(t, []topology.NetworkPathSegment{{Tier: 0, Name: "t0-77c1e0"}, {Tier: 1, Name: "t1-4b9f3a"}}, path.Segments())

	path, err = parseTopologyLabels(nil)
	require.NoError(t, err)
	assert.Empty(t, path.Segments())

	_, err = parseTopologyLabels(topologyLabels{"rack": "r1"})
	assert.ErrorContains(t, err, `parse "rack"`)
	_, err = parseTopologyLabels(topologyLabels{"tier-01": "t1"})
	assert.ErrorContains(t, err, `parse "tier-01"`)
	_, err = parseTopologyLabels(topologyLabels{"tier-x": "t1"})
	assert.ErrorContains(t, err, `parse "tier-x"`)
}

func Test_newNetworkPaths_Basic(t *testing.T) {
	labels := map[string]topologyLabels{
		"node-a": {"tier-0": "t0-77c1e0", "tier-1": "t1-4b9f3a"},
		"node-b": {"tier-0": "t0-b4d2f0", "tier-2": "t2-e2d6c5"},
	}
	nodeSet := testNodeSet("worker", 3, true, "gpucluster-2a9e")
	r, _, _ := newTestReconciler(t,
		testPod("worker-0", "worker", "node-a"),
		testPod("worker-1", "worker", ""),
		testPod("worker-2", "worker", "node-b"),
		testPod("worker-3", "worker", "node-a"),
		testPod("worker-rack0-0", "worker-rack0", "node-a"),
		testPod("login-0", "login", "node-a"),
	)

	paths, invalid, err := r.newNetworkPaths(context.Background(), logr.Discard(), *nodeSet, types.NamespacedName{Namespace: testNamespace, Name: "slurm"}, labels)
	require.NoError(t, err)
	assert.Len(t, paths, 1, "only worker-0 is scheduled, within the replicas and on a node with valid labels")
	assert.Equal(t, "tier-0=t0-77c1e0/tier-1=t1-4b9f3a", paths["worker-0"].String())
	assert.Equal(t, []string{`node "node-b": validate tier-2 segment "t2-e2d6c5": tier-1 below it is missing`}, invalid)
}

func Test_newTopoNodeSets_Basic(t *testing.T) {
	labels := map[string]topologyLabels{
		"node-a": {"tier-0": "t0-77c1e0"},
		"node-b": {"tier-0": "t0-b4d2f0", "tier-2": "t2-e2d6c5"},
	}
	r, _, recorder := newTestReconciler(t,
		testPod("worker-0", "worker", "node-a"),
		testPod("worker-1", "worker", "node-b"),
		testPod("cpu-0", "cpu", "node-b"),
	)
	nodeSets := []v1alpha1.NodeSet{
		*testNodeSet("worker", 2, true, "gpucluster-2a9e"),
		*testNodeSet("cpu", 1, false, ""),
	}

	topoNodeSets, err := r.newTopoNodeSets(context.Background(), logr.Discard(), testCluster, nodeSets, labels)
	require.NoError(t, err)
	require.Len(t, topoNodeSets, 2)
	assert.Equal(t, "worker", topoNodeSets[0].Name())
	assert.Equal(t, "cpu", topoNodeSets[1].Name(), "a CPU-only NodeSet is kept, without paths")

	recorded := drainEvents(recorder)
	require.Len(t, recorded, 1)
	assert.Contains(t, recorded[0], reasonInvalidNodeTopologyLabels)
	assert.Contains(t, recorded[0], `node "node-b"`)
}

func Test_recordEmptyTopologies_Basic(t *testing.T) {
	r, _, recorder := newTestReconciler(t)
	flat := topology.FlatTopology(true)
	r.recordEmptyTopologies(testCluster, topology.MultiTopology{
		{Name: "flat", Flat: &flat},
		{Name: "tree", Tree: &topology.TreeTopology{Switches: []topology.Switch{{Name: "unknown"}}}},
		{Name: "block", Block: &topology.BlockTopology{Blocks: []topology.Block{{Name: "t0-a3f0b9", Nodes: "worker-rack0-[0-17]"}}}},
	})

	recorded := drainEvents(recorder)
	require.Len(t, recorded, 1)
	assert.Contains(t, recorded[0], reasonTopologyReachesNoNode)
	assert.Contains(t, recorded[0], `"tree"`)
}

// The scenarios below run Reconcile over a small GPU cluster: a tree topology over the "worker"
// NodeSet as the cluster default and a flat one over "cpu".

func testClusterObjects(t *testing.T) []client.Object {
	t.Helper()
	cluster := testSlurmCluster(
		slurmv1.NamedTopology{Name: "tree", Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeTree}, ClusterDefault: new(true), NodeSetRefs: []string{"worker"}},
		slurmv1.NamedTopology{Name: "flat", Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeFlat}, NodeSetRefs: []string{"cpu"}},
	)
	return []client.Object{
		cluster,
		testNodeSet("worker", 2, true, "gpucluster-2a9e"),
		testNodeSet("cpu", 1, false, ""),
		testLabelsConfigMap(t, map[string]map[string]string{
			"node-a": {"tier-0": "t0-77c1e0", "tier-1": "t1-4b9f3a"},
			"node-b": {"tier-0": "t0-b4d2f0", "tier-1": "t1-4b9f3a"},
		}),
		testPod("worker-0", "worker", "node-a"),
		testPod("worker-1", "worker", ""),
		testPod("cpu-0", "cpu", "node-b"),
	}
}

func confirmReconfigure(t *testing.T, c client.Client) {
	t.Helper()
	jailedConfig := getJailedConfig(t, c)
	meta.SetStatusCondition(&jailedConfig.Status.Conditions, metav1.Condition{
		Type: string(v1alpha1.ReconfigurePerformed), Status: metav1.ConditionTrue, Reason: "Success",
		ObservedGeneration: jailedConfig.Generation,
	})
	require.NoError(t, c.Status().Update(context.Background(), jailedConfig))
}

func TestReconcileIgnoresClustersWithoutTopologies(t *testing.T) {
	r, c, recorder := newTestReconciler(t, testSlurmCluster())
	result := reconcileTestCluster(t, r)
	assert.Equal(t, ctrl.Result{RequeueAfter: defaultInterval}, result)
	assert.True(t, errors.IsNotFound(c.Get(context.Background(), testTopologyConfigKey, &corev1.ConfigMap{})), "nothing published")
	assert.Empty(t, drainEvents(recorder))

	r, _, _ = newTestReconciler(t)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "missing", Namespace: testNamespace},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result, "a deleted cluster is not requeued")
}

func TestReconcilePublishesNewTopologyWithReconfigureRequest(t *testing.T) {
	r, c, recorder := newTestReconciler(t, testClusterObjects(t)...)

	result := reconcileTestCluster(t, r)
	assert.Equal(t, ctrl.Result{RequeueAfter: defaultInterval}, result)

	content := getTopologyConfigMap(t, c).Data[consts.ConfigMapKeyTopologyYAML]
	assert.Contains(t, content, "- topology: tree\n  cluster_default: true\n  tree:")
	assert.Contains(t, content, "- topology: flat\n  cluster_default: false\n  flat: true")
	assert.NotContains(t, content, "cpu-0", "CPU-only nodes are listed by no fabric topology")
	assert.Equal(t, map[string]string{
		"gpucluster-2a9e":         "",
		"t1-4b9f3a":               "",
		"t0-77c1e0":               "worker-0",
		"gpucluster-2a9e.unknown": "worker-1",
	}, publishedSwitchNodes(t, c, "tree"))

	jailedConfig := getJailedConfig(t, c)
	assert.Equal(t, []v1alpha1.UpdateAction{v1alpha1.UpdateActionReconfigure}, jailedConfig.Spec.UpdateActions)
	assert.False(t, timeAnnotation(jailedConfig, annotationLastReconfigure).IsZero())

	recorded := drainEvents(recorder)
	require.Len(t, recorded, 1)
	assert.Contains(t, recorded[0], reasonReconfigureRequested)

	// Nothing changed: the content stays, the unconfirmed request stands, nothing is recorded.
	published := getTopologyConfigMap(t, c).ResourceVersion
	result = reconcileTestCluster(t, r)
	assert.Equal(t, ctrl.Result{RequeueAfter: defaultInterval}, result)
	assert.Equal(t, published, getTopologyConfigMap(t, c).ResourceVersion)
	assert.Equal(t, []v1alpha1.UpdateAction{v1alpha1.UpdateActionReconfigure}, getJailedConfig(t, c).Spec.UpdateActions)
	assert.Empty(t, drainEvents(recorder))
}

func TestReconcilePublishesNodeMovesWithoutReconfigure(t *testing.T) {
	r, c, recorder := newTestReconciler(t, testClusterObjects(t)...)
	reconcileTestCluster(t, r)
	confirmReconfigure(t, c)
	drainEvents(recorder)

	// worker-1 is scheduled on node-a, under the leaf switch already in the tree.
	pod := &corev1.Pod{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "worker-1", Namespace: testNamespace}, pod))
	pod.Spec.NodeName = "node-a"
	require.NoError(t, c.Update(context.Background(), pod))

	result := reconcileTestCluster(t, r)
	assert.Equal(t, ctrl.Result{RequeueAfter: defaultInterval}, result)
	switches := publishedSwitchNodes(t, c, "tree")
	assert.Equal(t, "worker-[0-1]", switches["t0-77c1e0"])
	assert.NotContains(t, switches, "gpucluster-2a9e.unknown")

	jailedConfig := getJailedConfig(t, c)
	assert.Empty(t, jailedConfig.Spec.UpdateActions, "the confirmed request is withdrawn and no new one is raised")
	assert.Empty(t, drainEvents(recorder))
}

func TestReconcileDefersReconfigureForANewLeafSwitch(t *testing.T) {
	r, c, recorder := newTestReconciler(t, testClusterObjects(t)...)
	reconcileTestCluster(t, r)
	confirmReconfigure(t, c)
	drainEvents(recorder)

	// worker-1 is scheduled on node-b, under a leaf switch new to the tree.
	pod := &corev1.Pod{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "worker-1", Namespace: testNamespace}, pod))
	pod.Spec.NodeName = "node-b"
	require.NoError(t, c.Update(context.Background(), pod))

	result := reconcileTestCluster(t, r)
	assert.Equal(t, ctrl.Result{RequeueAfter: defaultInterval}, result, "the wait is capped by the heartbeat")
	assert.Equal(t, "worker-1", publishedSwitchNodes(t, c, "tree")["t0-b4d2f0"], "the content is published at once")

	jailedConfig := getJailedConfig(t, c)
	assert.Empty(t, jailedConfig.Spec.UpdateActions, "the request is held")
	assert.False(t, timeAnnotation(jailedConfig, annotationDeferredSince).IsZero())
	assert.False(t, timeAnnotation(jailedConfig, annotationDeferredLastChange).IsZero())

	recorded := drainEvents(recorder)
	require.Len(t, recorded, 1)
	assert.Contains(t, recorded[0], reasonReconfigureDeferred)

	// Idle passes keep waiting without booking again.
	reconcileTestCluster(t, r)
	assert.Empty(t, getJailedConfig(t, c).Spec.UpdateActions)
	assert.Empty(t, drainEvents(recorder))
}
