package sconfigcontroller

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	k8srest "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	fakes "nebius.ai/slurm-operator/internal/controller/sconfigcontroller/fake"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
	slurmpattern "nebius.ai/slurm-operator/internal/utils/slurm/pattern"
)

const (
	slurmData    = "slurm data"
	topologyData = "topology data"

	slurmConfigsName = "slurm-configs"
	topologyName     = "topology-config"
	slurmConfPath    = "/etc/slurm/slurm.conf"
	topologyPath     = "/etc/slurm/topology.yaml"
)

type aggregatedGroup struct {
	sctrl    *JailedConfigReconciler
	slurmapi *slurmapifake.MockClient
	fs       *fakes.MockFs
	clock    *fakes.MockClock
	client   client.Client
	recorder *events.FakeRecorder
}

// newAggregatedGroup builds the real pairing: a slurm configs JailedConfig that always carries
// Reconfigure, and a topology one that never does, both in the same aggregation group.
func newAggregatedGroup(t *testing.T) *aggregatedGroup {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))

	labels := map[string]string{
		consts.LabelJailedAggregationKey: consts.LabelJailedAggregationCommonValue,
		consts.LabelInstanceKey:          "test-cluster",
	}
	cm := func(name, data string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Data:       map[string]string{"k": data},
		}
	}
	jc := func(name, path string, actions []slurmv1alpha1.UpdateAction) *slurmv1alpha1.JailedConfig {
		return &slurmv1alpha1.JailedConfig{
			TypeMeta:   metav1.TypeMeta{Kind: "JailedConfig", APIVersion: "slurm.nebius.ai/v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, Labels: labels},
			Spec: slurmv1alpha1.JailedConfigSpec{
				ConfigMap:     slurmv1alpha1.ConfigMapReference{Name: name},
				Items:         []corev1.KeyToPath{{Key: "k", Path: path}},
				UpdateActions: actions,
			},
		}
	}

	slurmJC := jc(slurmConfigsName, slurmConfPath, []slurmv1alpha1.UpdateAction{slurmv1alpha1.UpdateActionReconfigure})
	topoJC := jc(topologyName, topologyPath, nil)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(slurmJC, topoJC).
		WithRuntimeObjects(cm(slurmConfigsName, slurmData), slurmJC, cm(topologyName, topologyData), topoJC).
		Build()

	mgr, err := ctrl.NewManager(&k8srest.Config{}, ctrl.Options{
		Scheme:    scheme,
		NewClient: func(_ *rest.Config, _ client.Options) (client.Client, error) { return fakeClient, nil },
	})
	require.NoError(t, err)

	g := &aggregatedGroup{
		slurmapi: slurmapifake.NewMockClient(t),
		fs:       fakes.NewMockFs(t),
		clock:    fakes.NewMockClock(t),
		client:   fakeClient,
	}
	g.recorder = events.NewFakeRecorder(100)
	g.sctrl = NewJailedConfigReconciler(mgr.GetClient(), mgr.GetScheme(), "test-cluster",
		g.recorder, g.slurmapi, g.fs, time.Second, time.Minute)
	g.sctrl.clock = g.clock
	return g
}

func (g *aggregatedGroup) expectWrites(t *testing.T) {
	t.Helper()
	prepareFs(g.fs, "/etc/slurm", slurmConfPath, []byte(slurmData), os.FileMode(0o644))
	prepareFs(g.fs, "/etc/slurm", topologyPath, []byte(topologyData), os.FileMode(0o644))
}

func (g *aggregatedGroup) reconcile(t *testing.T, name string) {
	t.Helper()
	_, err := g.sctrl.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: testNamespace},
	})
	require.NoError(t, err)
}

// TestAggregatedReconfigureOnlyOnRealChange pins the rule the whole group depends on: a config that
// changed and asks for a reconfigure gets one, and nothing else does. The slurmapi mock has no
// expectations unless a reconfigure is expected, so an unwanted call fails the test.
func TestAggregatedReconfigureOnlyOnRealChange(t *testing.T) {
	t.Run("a topology-only change does not reconfigure", func(t *testing.T) {
		g := newAggregatedGroup(t)
		g.expectWrites(t)

		// The slurm configs member carries Reconfigure permanently, but it did not change, so the
		// group must stay quiet.
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)

		g.reconcile(t, topologyName)
	})

	t.Run("nothing changed at all does not reconfigure", func(t *testing.T) {
		g := newAggregatedGroup(t)
		g.expectWrites(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		markApplied(t, g, topologyName, topologyData, topologyPath)

		g.reconcile(t, topologyName)
	})

	t.Run("a changed slurm config reconfigures", func(t *testing.T) {
		g := newAggregatedGroup(t)
		g.expectWrites(t)
		markApplied(t, g, topologyName, topologyData, topologyPath)
		prepareSlurmApi(g.slurmapi, g.clock)

		g.reconcile(t, slurmConfigsName)
	})

	t.Run("a request over identical content is still satisfied", func(t *testing.T) {
		// A structural change can render byte-identical -- a topology gaining an entry that reaches
		// no node yet. Gating on the hash alone would leave the request permanently unsatisfied.
		g := newAggregatedGroup(t)
		g.expectWrites(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		markApplied(t, g, topologyName, topologyData, topologyPath)
		requestReconfigure(t, g, topologyName)
		prepareSlurmApi(g.slurmapi, g.clock)

		g.reconcile(t, topologyName)
	})

	t.Run("a topology change asking for it reconfigures", func(t *testing.T) {
		g := newAggregatedGroup(t)
		g.expectWrites(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		requestReconfigure(t, g, topologyName)
		prepareSlurmApi(g.slurmapi, g.clock)

		g.reconcile(t, topologyName)
	})
}

// requestReconfigure is how the topology controller signals a structural change: the topology
// config asks for a reconfigure only then, never on a node moving between switches.
func requestReconfigure(t *testing.T, g *aggregatedGroup, name string) {
	t.Helper()
	jc := &slurmv1alpha1.JailedConfig{}
	require.NoError(t, g.client.Get(context.Background(),
		types.NamespacedName{Name: name, Namespace: testNamespace}, jc))
	jc.Spec.UpdateActions = []slurmv1alpha1.UpdateAction{slurmv1alpha1.UpdateActionReconfigure}
	// The fake client does not maintain Generation, so the bump a real API server performs on a
	// spec change is applied by hand. Without it the request would look already confirmed.
	jc.Generation++
	require.NoError(t, g.client.Update(context.Background(), jc))
}

// markApplied stands in for a completed previous reconciliation: the payload was written, and any
// reconfigure it asked for was confirmed for the generation it asked at.
func markApplied(t *testing.T, g *aggregatedGroup, name, data, path string) {
	t.Helper()
	jc := &slurmv1alpha1.JailedConfig{}
	require.NoError(t, g.client.Get(context.Background(),
		types.NamespacedName{Name: name, Namespace: testNamespace}, jc))
	jc.Status.AppliedHash = payloadHash(map[string]JailedFile{
		path: {Data: []byte(data), Mode: slurmv1alpha1.DefaultMode},
	})
	meta.SetStatusCondition(&jc.Status.Conditions, metav1.Condition{
		Type:               string(slurmv1alpha1.ReconfigurePerformed),
		Status:             metav1.ConditionTrue,
		Reason:             slurmv1alpha1.ReasonSuccess,
		ObservedGeneration: jc.Generation,
	})
	require.NoError(t, g.client.Status().Update(context.Background(), jc))
}

// TestReconfigureEmitsEvent pins that a reconfigure is visible through the Kubernetes API and not
// only in operator logs: `kubectl describe jailedconfig` shows when slurmctld last re-read a config.
func TestReconfigureEmitsEvent(t *testing.T) {
	t.Run("a reconfigure is recorded against the configs it covered", func(t *testing.T) {
		g := newAggregatedGroup(t)
		g.expectWrites(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		requestReconfigure(t, g, topologyName)
		prepareSlurmApi(g.slurmapi, g.clock)

		g.reconcile(t, topologyName)

		var events []string
		for len(g.recorder.Events) > 0 {
			events = append(events, <-g.recorder.Events)
		}

		require.NotEmpty(t, events, "a performed reconfigure must leave an event")
		for _, event := range events {
			assert.Contains(t, event, corev1.EventTypeNormal)
			assert.Contains(t, event, reasonReconfigured)
		}
	})

	t.Run("a pass that reconfigures nothing stays silent", func(t *testing.T) {
		g := newAggregatedGroup(t)
		g.expectWrites(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		markApplied(t, g, topologyName, topologyData, topologyPath)

		g.reconcile(t, topologyName)

		assert.Empty(t, g.recorder.Events, "no reconfigure ran, so there is nothing to report")
	})
}

// TestReconfigureIsDueReportsWhy pins that the trigger is nameable. A reconfigure restarts
// slurmctld, so an unexpected one must be explainable from the default logs and the event, without
// turning on debug after the fact.
func TestReconfigureIsDueReportsWhy(t *testing.T) {
	withRequest := func(hash string, generation int64) *slurmv1alpha1.JailedConfig {
		jc := &slurmv1alpha1.JailedConfig{}
		jc.Generation = generation
		jc.Spec.UpdateActions = []slurmv1alpha1.UpdateAction{slurmv1alpha1.UpdateActionReconfigure}
		jc.Status.AppliedHash = hash
		return jc
	}

	t.Run("a config that asks for nothing is not due and needs no reason", func(t *testing.T) {
		due, reason := reconfigureIsDue(&slurmv1alpha1.JailedConfig{}, "h1")

		assert.False(t, due)
		assert.Empty(t, reason)
	})

	t.Run("changed content names itself", func(t *testing.T) {
		due, reason := reconfigureIsDue(withRequest("h1", 1), "h2")

		assert.True(t, due)
		assert.Equal(t, reconfigureReasonContentChanged, reason)
	})

	t.Run("an unsatisfied request names itself", func(t *testing.T) {
		// Same content, but no reconfigure has been confirmed for this generation yet.
		due, reason := reconfigureIsDue(withRequest("h1", 1), "h1")

		assert.True(t, due)
		assert.Equal(t, reconfigureReasonUnsatisfied, reason)
	})

	t.Run("a satisfied request over unchanged content is not due", func(t *testing.T) {
		jc := withRequest("h1", 7)
		jc.Status.Conditions = []metav1.Condition{{
			Type:               string(slurmv1alpha1.ReconfigurePerformed),
			Status:             metav1.ConditionTrue,
			Reason:             slurmv1alpha1.ReasonSuccess,
			ObservedGeneration: 7,
		}}

		due, reason := reconfigureIsDue(jc, "h1")

		assert.False(t, due)
		assert.Empty(t, reason)
	})
}

// The nodes that never restarted are the point of the timeout error: without them it says only
// that something, somewhere, did not come back.
func TestPendingNodeNamesAreSortedForTheTimeoutError(t *testing.T) {
	names := pendingNodeNames(map[string]int64{
		"worker-2": 3,
		"worker-0": 1,
		"worker-1": 2,
	})

	assert.Equal(t, []string{"worker-0", "worker-1", "worker-2"}, names)
	assert.Equal(t, "worker-[0-2]", slurmpattern.Merge(names))
}

func TestPendingNodeNamesOfAnEmptyMap(t *testing.T) {
	assert.Empty(t, pendingNodeNames(map[string]int64{}))
}
