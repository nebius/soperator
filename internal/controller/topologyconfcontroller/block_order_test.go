package topologyconfcontroller

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

func TestBlockOrderFollowsIBHierarchy(t *testing.T) {
	cluster := clusterWithTopologies(slurmv1.NamedTopology{
		Name: "blocks", Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeBlock},
		NodeSetRefs: []string{consts.SlurmTopologyNodeSetRefAll},
	})
	nodeSets := []v1alpha1.NodeSet{
		gpuNodeSet("rack-a", 2, ""), gpuNodeSet("rack-b", 2, ""),
		gpuNodeSet("rack-c", 2, ""), gpuNodeSet("rack-d", 2, ""),
		gpuNodeSet("rack-e", 2, ""), gpuNodeSet("rack-f", 2, ""),
	}
	labels := nodeLabelsCM(map[string]string{
		"a": `{"tier-0":"block-a","tier-1":"leaf-a","tier-2":"spine-a","tier-3":"core-b"}`,
		"b": `{"tier-0":"block-b","tier-1":"leaf-b","tier-2":"spine-z","tier-3":"core-a"}`,
		"c": `{"tier-0":"block-c","tier-1":"leaf-z","tier-2":"spine-a","tier-3":"core-a"}`,
		"d": `{"tier-0":"block-d","tier-1":"leaf-z","tier-2":"spine-a","tier-3":"core-a"}`,
		"e": `{"tier-0":"block-e","tier-1":"leaf-a","tier-2":"spine-a","tier-3":"core-a"}`,
		"f": `{"tier-0":"block-f"}`,
	})
	pods := make(map[string][]string)
	for _, rack := range []string{"a", "b", "c", "d", "e", "f"} {
		pods[rack] = []string{"rack-" + rack + "-1", "rack-" + rack + "-0"}
	}
	r := &WorkerTopologyReconciler{}
	rendered, err := r.buildMultiTopologyYAML(context.Background(), cluster, nodeSets, labels, pods)
	require.NoError(t, err)
	var entries []topologyYAMLEntry
	require.NoError(t, yaml.Unmarshal([]byte(rendered), &entries))
	require.Equal(t, []blockYAML{
		{Block: "block-e", Nodes: "rack-e-[0-1]"},
		{Block: "block-c", Nodes: "rack-c-[0-1]"},
		{Block: "block-d", Nodes: "rack-d-[0-1]"},
		{Block: "block-b", Nodes: "rack-b-[0-1]"},
		{Block: "block-a", Nodes: "rack-a-[0-1]"},
		{Block: "block-f", Nodes: "rack-f-[0-1]"},
		{Block: "unknown"},
	}, entries[0].Block.Blocks)
	slices.Reverse(nodeSets)
	repeated, err := r.buildMultiTopologyYAML(context.Background(), cluster, nodeSets, labels, pods)
	require.NoError(t, err)
	assert.Equal(t, rendered, repeated)
}

func TestBlockOrderUsesEachBlockWithinOneNodeSet(t *testing.T) {
	cluster := clusterWithTopologies(slurmv1.NamedTopology{
		Name: "blocks", Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeBlock},
		NodeSetRefs: []string{consts.SlurmTopologyNodeSetRefAll},
	})
	labels := nodeLabelsCM(map[string]string{
		"a": `{"tier-0":"block-a","tier-1":"leaf-z"}`,
		"b": `{"tier-0":"block-z","tier-1":"leaf-a"}`,
	})
	recorder := events.NewFakeRecorder(10)
	r := &WorkerTopologyReconciler{recorder: recorder}
	rendered, err := r.buildMultiTopologyYAML(context.Background(), cluster,
		[]v1alpha1.NodeSet{gpuNodeSet("worker", 4, "")}, labels,
		map[string][]string{"a": {"worker-0", "worker-1"}, "b": {"worker-2", "worker-3"}})
	require.NoError(t, err)
	var entries []topologyYAMLEntry
	require.NoError(t, yaml.Unmarshal([]byte(rendered), &entries))
	assert.Equal(t, []blockYAML{
		{Block: "block-z", Nodes: "worker-[2-3]"},
		{Block: "block-a", Nodes: "worker-[0-1]"},
		{Block: "unknown"},
	}, entries[0].Block.Blocks)
	assert.Empty(t, drainEvents(recorder))
}

func TestBlockOrderSurvivesPodChurn(t *testing.T) {
	labels := map[string]NodeTopologyLabels{
		"a": {"tier-0": "block-z", "tier-1": "leaf-a"},
		"b": {"tier-0": "block-z", "tier-1": "leaf-z"},
		"c": {"tier-0": "block-a", "tier-1": "leaf-m"},
	}
	allNodes := []string{"worker-0", "worker-1", "worker-2"}
	for name, pods := range map[string]map[string][]string{
		"all workers scheduled": {"a": {"worker-0"}, "b": {"worker-1"}, "c": {"worker-2"}},
		"first worker evicted":  {"b": {"worker-1"}, "c": {"worker-2"}},
		"worker rescheduled":    {"b": {"worker-0", "worker-1"}, "c": {"worker-2"}},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := events.NewFakeRecorder(10)
			r := &WorkerTopologyReconciler{recorder: recorder}
			topology := BuildTopologyBlocks(context.Background(), labels, pods, allNodes, nil)
			original := topology.RenderBlocks(nil)
			blocks := topology.RenderBlocks(blockIBPaths(labels))
			r.reportBlockConflicts(context.Background(), &slurmv1.SlurmCluster{},
				[]topologyYAMLEntry{{Block: &blockTopologyYAML{Blocks: blocks}}}, blockIBPaths(labels))
			assert.ElementsMatch(t, original, blocks, "sorting preserves node lists and block membership")
			assert.Equal(t, "block-z", blocks[0].Block)
			assert.Equal(t, "block-a", blocks[1].Block)
			captured := drainEvents(recorder)
			require.Len(t, captured, 1)
			assert.Contains(t, captured[0], reasonBlockIBTopologyConflict)
			assert.Contains(t, captured[0], `Kubernetes node "a"`)
		})
	}
}

func TestBlockIBPathsMissingLabels(t *testing.T) {
	paths := blockIBPaths(map[string]NodeTopologyLabels{
		"missing":        nil,
		"tier-zero-only": {"tier-0": "block-z"},
		"empty":          {"tier-0": "block-z", "tier-1": "leaf-a", "tier-2": ""},
		"invalid":        {"tier-0": "block-z", "tier-1": "leaf-a", "tier-bad": "bad"},
		"known":          {"tier-0": "block-z", "tier-1": "leaf-a", "tier-2": "spine", "tier-10": "core"},
		"no-leaf":        {"tier-0": "unknown-path", "tier-2": "spine"},
	})
	require.Len(t, paths, 2)
	assert.Equal(t, []topologyTier{{10, "core"}, {2, "spine"}}, paths["unknown-path"].path)
	assert.Equal(t, []topologyTier{{10, "core"}, {2, "spine"}, {1, "leaf-a"}}, paths["block-z"].path)
	assert.False(t, paths["block-z"].conflict)
}

func TestBlockIBPathsCompleteMissingAncestors(t *testing.T) {
	paths := blockIBPaths(map[string]NodeTopologyLabels{
		"a": {"tier-0": "block-a", "tier-1": "leaf1", "tier-2": "spineX", "tier-3": "core1"},
		"b": {"tier-0": "block-b", "tier-1": "leaf2", "tier-2": "spineX"},
		"c": {"tier-0": "block-c", "tier-1": "leaf3", "tier-2": "spineA", "tier-3": "core1"},
		"d": {"tier-0": "block-d", "tier-1": "leaf1", "tier-3": "core1"},
		"e": {"tier-0": "block-e", "tier-2": "spineX", "tier-3": "core1"},
	})
	assert.Equal(t, []topologyTier{{3, "core1"}, {2, "spineX"}, {1, "leaf2"}}, paths["block-b"].path)
	assert.Equal(t, paths["block-a"].path, paths["block-d"].path)
	assert.Equal(t, []topologyTier{{3, "core1"}, {2, "spineX"}}, paths["block-e"].path)
	blocks := []blockYAML{{Block: "block-a"}, {Block: "block-b"}, {Block: "block-c"}, {Block: "block-d"}, {Block: "block-e"}}
	sortBlocksByIBTopology(blocks, paths)
	assert.Equal(t, []blockYAML{{Block: "block-c"}, {Block: "block-a"}, {Block: "block-d"}, {Block: "block-b"}, {Block: "block-e"}}, blocks,
		"a path missing its lower tiers sorts after the fully labelled blocks under the same switch")
}

func TestBlockIBPathsDoNotInferAmbiguousParents(t *testing.T) {
	paths := blockIBPaths(map[string]NodeTopologyLabels{
		"a": {"tier-0": "block-a", "tier-1": "leaf-a", "tier-2": "spine", "tier-3": "core-a"},
		"b": {"tier-0": "block-b", "tier-1": "leaf-b", "tier-2": "spine", "tier-3": "core-b"},
		"c": {"tier-0": "block-c", "tier-1": "leaf-c", "tier-2": "spine"},
	})
	assert.Equal(t, []topologyTier{{2, "spine"}, {1, "leaf-c"}}, paths["block-c"].path)
	assert.Negative(t, compareIBPaths([]topologyTier{{3, "z"}}, []topologyTier{{2, "a"}}),
		"names at different tiers must not be compared")
}

func TestBlockConflictsReportedOnceAcrossTopologies(t *testing.T) {
	spec := slurmv1.NamedTopology{
		Name: "first", Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeBlock},
		NodeSetRefs: []string{consts.SlurmTopologyNodeSetRefAll},
	}
	second := spec
	second.Name = "second"
	recorder := events.NewFakeRecorder(10)
	r := &WorkerTopologyReconciler{recorder: recorder}
	_, err := r.buildMultiTopologyYAML(context.Background(), clusterWithTopologies(spec, second),
		[]v1alpha1.NodeSet{gpuNodeSet("worker", 2, "")}, nodeLabelsCM(map[string]string{
			"a":          `{"tier-0":"block","tier-1":"leaf-a"}`,
			"b":          `{"tier-0":"block","tier-1":"leaf-b"}`,
			"excluded-a": `{"tier-0":"excluded","tier-1":"leaf-x"}`,
			"excluded-b": `{"tier-0":"excluded","tier-1":"leaf-y"}`,
		}), map[string][]string{"a": {"worker-0"}, "b": {"worker-1"}})
	require.NoError(t, err)
	captured := drainEvents(recorder)
	require.Len(t, captured, 1)
	assert.Contains(t, captured[0], `block "block"`)
}

func TestBlockOrderTenThousandWorkers(t *testing.T) {
	const racks, workersPerRack = 500, 20
	var blocks []blockYAML
	labels := make(map[string]NodeTopologyLabels)
	for rack := range racks {
		name := fmt.Sprintf("rack-%03d", rack)
		blocks = append(blocks, blockYAML{Block: name})
		for worker := range workersPerRack {
			node := fmt.Sprintf("%s-%d", name, worker)
			labels[node] = NodeTopologyLabels{
				"tier-0": name, "tier-1": fmt.Sprintf("leaf-%03d", racks-rack-1),
			}
		}
	}
	paths := blockIBPaths(labels)
	sortBlocksByIBTopology(blocks, paths)
	for i, block := range blocks {
		assert.Equal(t, fmt.Sprintf("rack-%03d", racks-i-1), block.Block)
	}
	require.Len(t, paths, racks)
}

func TestBlockConflictsReportedOnChange(t *testing.T) {
	var output bytes.Buffer
	ctx := log.IntoContext(t.Context(), zap.New(zap.WriteTo(&output)))
	recorder := events.NewFakeRecorder(10)
	r := &WorkerTopologyReconciler{recorder: recorder}
	cluster := clusterWithTopologies(slurmv1.NamedTopology{
		Name: "blocks", Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeBlock},
		NodeSetRefs: []string{consts.SlurmTopologyNodeSetRefAll},
	})
	labels := nodeLabelsCM(map[string]string{
		"a": `{"tier-0":"block","tier-1":"leaf-a","tier-2":"spine"}`,
		"b": `{"tier-0":"block","tier-1":"leaf-b","tier-2":"spine"}`,
	})
	render := func() {
		_, err := r.buildMultiTopologyYAML(ctx, cluster, []v1alpha1.NodeSet{gpuNodeSet("worker", 2, "")},
			labels, map[string][]string{"a": {"worker-0"}, "b": {"worker-1"}})
		require.NoError(t, err)
	}
	render()
	render()
	captured := drainEvents(recorder)
	require.Len(t, captured, 1)
	assert.Contains(t, captured[0], "tier-2=spine/tier-1=leaf-a")
	assert.Contains(t, output.String(), "tier-2=spine/tier-1=leaf-a")
	assert.Equal(t, 1, strings.Count(output.String(), "Blocks have conflicting IB topology paths"))

	conflicting := labels.Data["b"]
	labels.Data["b"] = labels.Data["a"]
	render()
	assert.Empty(t, drainEvents(recorder))
	assert.Empty(t, r.blockConflicts)
	labels.Data["b"] = conflicting
	render()
	assert.Len(t, drainEvents(recorder), 1, "a recurring conflict is reported again after recovery")
}

func TestCompareIBPathsMissingTiersSortLast(t *testing.T) {
	full := []topologyTier{{3, "core-a"}, {2, "spine-a"}, {1, "leaf-a"}}
	noLowerTiers := []topologyTier{{3, "core-a"}}
	noMiddleTier := []topologyTier{{3, "core-a"}, {1, "leaf-z"}}
	otherCore := []topologyTier{{3, "core-b"}, {2, "spine-a"}, {1, "leaf-a"}}
	paths := [][]topologyTier{nil, otherCore, noLowerTiers, noMiddleTier, full}
	slices.SortFunc(paths, compareIBPaths)
	assert.Equal(t, [][]topologyTier{full, noMiddleTier, noLowerTiers, otherCore, nil}, paths)
}

func TestBlockConflictsAggregatedIntoOneEvent(t *testing.T) {
	var output bytes.Buffer
	ctx := log.IntoContext(t.Context(), zap.New(zap.WriteTo(&output)))
	recorder := events.NewFakeRecorder(10)
	r := &WorkerTopologyReconciler{recorder: recorder}
	const conflicting = maxReportedBlockConflicts + 3
	labels := make(map[string]NodeTopologyLabels)
	var blocks []blockYAML
	for i := range conflicting {
		block := fmt.Sprintf("block-%d", i)
		blocks = append(blocks, blockYAML{Block: block})
		labels[block+"-a"] = NodeTopologyLabels{"tier-0": block, "tier-1": "leaf-a"}
		labels[block+"-b"] = NodeTopologyLabels{"tier-0": block, "tier-1": "leaf-b"}
	}
	entries := []topologyYAMLEntry{{Block: &blockTopologyYAML{Blocks: blocks}}}
	r.reportBlockConflicts(ctx, &slurmv1.SlurmCluster{}, entries, blockIBPaths(labels))
	captured := drainEvents(recorder)
	require.Len(t, captured, 1)
	assert.Contains(t, captured[0], fmt.Sprintf("%d block(s)", conflicting))
	assert.Contains(t, captured[0], "and 3 more")
	assert.Equal(t, 1, strings.Count(output.String(), "Blocks have conflicting IB topology paths"))
}

func TestBlockConflictsForgotten(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(slurmv1.AddToScheme(scheme))
	cluster := clusterWithTopologies()
	cluster.Namespace, cluster.Name = "soperator", "slurm"
	key := client.ObjectKeyFromObject(cluster)
	for name, objects := range map[string][]client.Object{
		"topologies removed": {cluster},
		"cluster deleted":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			r := &WorkerTopologyReconciler{
				BaseReconciler: BaseReconciler{
					Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(),
					Scheme: scheme,
				},
				blockConflicts: map[types.NamespacedName]map[string]string{key: {"block": "conflict"}},
			}
			_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
			require.NoError(t, err)
			assert.Empty(t, r.blockConflicts)
		})
	}
}

func TestBlockIBPathsPreferMajorityThenCompletePath(t *testing.T) {
	paths := blockIBPaths(map[string]NodeTopologyLabels{
		"node-0": {"tier-0": "rack", "tier-2": "spine"},
		"node-a": {"tier-0": "rack", "tier-1": "leaf", "tier-2": "spine"},
		"node-b": {"tier-0": "rack", "tier-1": "leaf", "tier-2": "spine"},
		"tie-0":  {"tier-0": "tie", "tier-2": "spine"},
		"tie-a":  {"tier-0": "tie", "tier-1": "leaf", "tier-2": "spine"},
	})
	assert.Equal(t, blockIBPath{path: []topologyTier{{2, "spine"}, {1, "leaf"}}, node: "node-a", conflict: true}, paths["rack"])
	assert.Equal(t, blockIBPath{path: []topologyTier{{2, "spine"}, {1, "leaf"}}, node: "tie-a", conflict: true}, paths["tie"])
}

func TestBlockStructureStableWhenWorkerRescheduled(t *testing.T) {
	cluster := clusterWithTopologies(slurmv1.NamedTopology{
		Name: "blocks", Topo: slurmv1.TopologyPlugin{Type: consts.SlurmTopologyTypeBlock},
		NodeSetRefs: []string{consts.SlurmTopologyNodeSetRefAll},
	})
	labels := nodeLabelsCM(map[string]string{
		"a": `{"tier-0":"block-a","tier-1":"leaf-a"}`,
		"b": `{"tier-0":"block-b","tier-1":"leaf-b"}`,
	})
	r := &WorkerTopologyReconciler{}
	structure := func(pods map[string][]string) string {
		rendered, err := r.buildMultiTopologyYAML(context.Background(), cluster,
			[]v1alpha1.NodeSet{gpuNodeSet("worker", 4, "")}, labels, pods)
		require.NoError(t, err)
		result, err := topologyStructure(rendered)
		require.NoError(t, err)
		return result
	}
	allPlaced := structure(map[string][]string{"a": {"worker-0", "worker-1"}, "b": {"worker-2", "worker-3"}})
	oneEvicted := structure(map[string][]string{"a": {"worker-0", "worker-1"}, "b": {"worker-2"}})
	assert.Equal(t, allPlaced, oneEvicted, "a single rescheduled worker must not add or remove the unknown block")
	rackDown := structure(map[string][]string{"a": {"worker-0", "worker-1"}})
	assert.NotEqual(t, allPlaced, rackDown, "a block disappearing requests a reconfigure")
}

func TestUnknownBlocksSortLast(t *testing.T) {
	blocks := []blockYAML{{Block: "vr-01"}, {Block: "a-fab.unknown"}, {Block: "block-a"}, {Block: "unknown"}}
	sortBlocksByIBTopology(blocks, nil)
	assert.Equal(t, []blockYAML{{Block: "block-a"}, {Block: "vr-01"}, {Block: "a-fab.unknown"}, {Block: "unknown"}}, blocks)
}

func TestBlockConflictEventFitsNoteLimit(t *testing.T) {
	recorder := events.NewFakeRecorder(10)
	r := &WorkerTopologyReconciler{recorder: recorder}
	long := strings.Repeat("f", 64)
	labels := make(map[string]NodeTopologyLabels)
	var blocks []blockYAML
	for i := range maxReportedBlockConflicts {
		block := fmt.Sprintf("%s-%d", long, i)
		blocks = append(blocks, blockYAML{Block: block})
		for _, leaf := range []string{"a", "b"} {
			labels[block+"-computeinstance-"+leaf] = NodeTopologyLabels{
				"tier-0": block, "tier-1": long + leaf, "tier-2": long, "tier-3": long,
			}
		}
	}
	r.reportBlockConflicts(context.Background(), &slurmv1.SlurmCluster{},
		[]topologyYAMLEntry{{Block: &blockTopologyYAML{Blocks: blocks}}}, blockIBPaths(labels))
	captured := drainEvents(recorder)
	require.Len(t, captured, 1)
	note := strings.TrimPrefix(captured[0], "Warning "+reasonBlockIBTopologyConflict+" ")
	assert.LessOrEqual(t, len(note), eventNoteLimit)
	assert.True(t, strings.HasSuffix(note, "..."))
}

func TestBlockConflictNotReportedForNewRepresentativeNode(t *testing.T) {
	recorder := events.NewFakeRecorder(10)
	r := &WorkerTopologyReconciler{recorder: recorder}
	entries := []topologyYAMLEntry{{Block: &blockTopologyYAML{Blocks: []blockYAML{{Block: "block"}}}}}
	report := func(labels map[string]NodeTopologyLabels) {
		r.reportBlockConflicts(context.Background(), &slurmv1.SlurmCluster{}, entries, blockIBPaths(labels))
	}
	report(map[string]NodeTopologyLabels{
		"node-a": {"tier-0": "block", "tier-1": "leaf-a"},
		"node-b": {"tier-0": "block", "tier-1": "leaf-a"},
		"node-c": {"tier-0": "block", "tier-1": "leaf-c"},
	})
	require.Len(t, drainEvents(recorder), 1)
	report(map[string]NodeTopologyLabels{
		"node-d": {"tier-0": "block", "tier-1": "leaf-a"},
		"node-b": {"tier-0": "block", "tier-1": "leaf-a"},
		"node-c": {"tier-0": "block", "tier-1": "leaf-c"},
	})
	assert.Empty(t, drainEvents(recorder), "the selected path is unchanged")
}
