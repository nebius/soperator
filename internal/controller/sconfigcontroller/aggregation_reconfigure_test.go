package sconfigcontroller

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	fakes "nebius.ai/slurm-operator/internal/controller/sconfigcontroller/fake"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
	"nebius.ai/slurm-operator/internal/utils/jailedpayload"
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
	writes   statusWrites
}

// statusWrite is a JailedConfig as the API server holds it right after one status patch: what a
// watcher such as the topology controller can observe between two patches.
type statusWrite struct {
	generation int64
	status     slurmv1alpha1.JailedConfigStatus
}

func (w statusWrite) reconfigurePerformed() *metav1.Condition {
	return meta.FindStatusCondition(w.status.Conditions, string(slurmv1alpha1.ReconfigurePerformed))
}

// confirms tells whether this status reads as a reconfigure over the payload with the given hash,
// the way the topology controller reads it before withdrawing its request.
func (w statusWrite) confirms(hash string) bool {
	condition := w.reconfigurePerformed()
	return condition != nil && condition.Status == metav1.ConditionTrue &&
		condition.ObservedGeneration == w.generation &&
		w.status.AppliedHash == hash
}

// statusWrites records every JailedConfig status patch, by JailedConfig name.
type statusWrites map[string][]statusWrite

func (w statusWrites) interceptor() interceptor.Funcs {
	return interceptor.Funcs{
		SubResourcePatch: func(
			ctx context.Context, c client.Client, subResourceName string,
			obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption,
		) error {
			if err := c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...); err != nil {
				return err
			}
			if jailedConfig, ok := obj.(*slurmv1alpha1.JailedConfig); ok && subResourceName == "status" {
				w[jailedConfig.Name] = append(w[jailedConfig.Name], statusWrite{
					generation: jailedConfig.Generation,
					status:     *jailedConfig.Status.DeepCopy(),
				})
			}
			return nil
		},
	}
}

// performed is the writes of the named JailedConfig that report a performed reconfigure.
func (w statusWrites) performed(name string) []statusWrite {
	var performed []statusWrite
	for _, write := range w[name] {
		if condition := write.reconfigurePerformed(); condition != nil && condition.Status == metav1.ConditionTrue {
			performed = append(performed, write)
		}
	}
	return performed
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

	writes := statusWrites{}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(slurmJC, topoJC).
		WithRuntimeObjects(cm(slurmConfigsName, slurmData), slurmJC, cm(topologyName, topologyData), topoJC).
		WithInterceptorFuncs(writes.interceptor()).
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
		writes:   writes,
	}
	g.recorder = events.NewFakeRecorder(100)
	g.sctrl = NewJailedConfigReconciler(mgr.GetClient(), mgr.GetScheme(), "test-cluster",
		g.recorder, g.slurmapi, g.fs, time.Second, time.Minute)
	g.sctrl.clock = g.clock
	return g
}

func (g *aggregatedGroup) expectWrites(t *testing.T) {
	t.Helper()
	g.expectWritesWithTopology(t, topologyData)
}

// expectWritesWithTopology expects the group's files to be written with the given topology, and
// returns the calls replacing the topology file.
func (g *aggregatedGroup) expectWritesWithTopology(t *testing.T, topology string) fileReplacement {
	t.Helper()
	prepareFs(g.fs, "/etc/slurm", slurmConfPath, []byte(slurmData), os.FileMode(0o644))
	return prepareFs(g.fs, "/etc/slurm", topologyPath, []byte(topology), os.FileMode(0o644))
}

// nextPass gives the group fresh fs and Slurm API mocks, so a further reconciliation states its own
// expectations instead of matching the ones left by the previous pass.
func (g *aggregatedGroup) nextPass(t *testing.T) {
	t.Helper()
	g.slurmapi = slurmapifake.NewMockClient(t)
	g.fs = fakes.NewMockFs(t)
	g.clock = fakes.NewMockClock(t)
	g.sctrl.slurmAPIClient = g.slurmapi
	g.sctrl.fs = g.fs
	g.sctrl.clock = g.clock
}

// publishTopology is the topology controller writing new content to the topology ConfigMap.
func (g *aggregatedGroup) publishTopology(t *testing.T, topology string) {
	t.Helper()
	configMap := &corev1.ConfigMap{}
	require.NoError(t, g.client.Get(context.Background(),
		types.NamespacedName{Name: topologyName, Namespace: testNamespace}, configMap))
	configMap.Data = map[string]string{"k": topology}
	require.NoError(t, g.client.Update(context.Background(), configMap))
}

func (g *aggregatedGroup) get(t *testing.T, name string) *slurmv1alpha1.JailedConfig {
	t.Helper()
	jailedConfig := &slurmv1alpha1.JailedConfig{}
	require.NoError(t, g.client.Get(context.Background(),
		types.NamespacedName{Name: name, Namespace: testNamespace}, jailedConfig))
	return jailedConfig
}

// bumpGeneration stands in for a spec change landing on the API server. The fake client does not
// maintain Generation, so the bump is applied by hand.
func bumpGeneration(t *testing.T, c client.Client, name string) {
	t.Helper()
	jailedConfig := &slurmv1alpha1.JailedConfig{}
	require.NoError(t, c.Get(context.Background(),
		types.NamespacedName{Name: name, Namespace: testNamespace}, jailedConfig))
	jailedConfig.Generation++
	require.NoError(t, c.Update(context.Background(), jailedConfig))
}

func payloadHash(path, data string) string {
	return jailedpayload.Hash(map[string]jailedpayload.File{
		path: {Data: []byte(data), Mode: slurmv1alpha1.DefaultMode},
	})
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
		requestReconfigure(t, g)
		prepareSlurmApi(g.slurmapi, g.clock)

		g.reconcile(t, topologyName)
	})

	t.Run("a topology change asking for it reconfigures", func(t *testing.T) {
		g := newAggregatedGroup(t)
		g.expectWrites(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		requestReconfigure(t, g)
		prepareSlurmApi(g.slurmapi, g.clock)

		g.reconcile(t, topologyName)
	})
}

// requestReconfigure is how the topology controller signals a structural change: the topology
// config asks for a reconfigure only then, never on a node moving between switches.
func requestReconfigure(t *testing.T, g *aggregatedGroup) {
	t.Helper()
	jc := &slurmv1alpha1.JailedConfig{}
	require.NoError(t, g.client.Get(context.Background(),
		types.NamespacedName{Name: topologyName, Namespace: testNamespace}, jc))
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
	jc.Status.AppliedHash = payloadHash(path, data)
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
		requestReconfigure(t, g)
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

// TestAggregatedReconfigureDoesNotConfirmANewerTopology covers a reconfigure still running when the
// published topology changes again. slurmctld re-reads what was written before the reconfigure
// started, so its report must not let the topology controller take it for one over the newer file.
const (
	topologyNVLink  = "topology with the NVLink block"
	topologyUnknown = "topology with only the unknown block"
)

func TestAggregatedReconfigureDoesNotConfirmANewerTopology(t *testing.T) {
	t.Run("a topology going A -> unknown -> A during a reconfigure keeps its request", func(t *testing.T) {
		g := newAggregatedGroup(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		// Slurm runs A when the topology falls back to unknown and asks for a reconfigure.
		markApplied(t, g, topologyName, topologyNVLink, topologyPath)
		requestReconfigure(t, g)
		g.publishTopology(t, topologyUnknown)
		requestedGeneration := g.get(t, topologyName).Generation

		// The topology returns to A while slurmctld re-reads unknown.
		g.expectWritesWithTopology(t, topologyUnknown)
		prepareSlurmApi(g.slurmapi, g.clock).
			Run(func(mock.Arguments) { g.publishTopology(t, topologyNVLink) })

		g.reconcile(t, topologyName)

		var performed []statusWrite
		for _, write := range g.writes[topologyName] {
			assert.False(t, write.confirms(payloadHash(topologyPath, topologyNVLink)),
				"slurmctld read unknown, yet this status confirms A: %+v", write)
			if condition := write.reconfigurePerformed(); condition != nil &&
				condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == requestedGeneration {
				performed = append(performed, write)
			}
		}
		require.NotEmpty(t, performed, "the reconfigure must be reported")
		for _, write := range performed {
			assert.Equal(t, payloadHash(topologyPath, topologyUnknown), write.status.AppliedHash,
				"ReconfigurePerformed must never be visible next to a hash it did not read")
		}

		// The request still stands, so the next pass writes A and reconfigures again.
		g.nextPass(t)
		g.expectWritesWithTopology(t, topologyNVLink)
		prepareSlurmApi(g.slurmapi, g.clock)

		g.reconcile(t, topologyName)

		writes := g.writes[topologyName]
		assert.True(t, writes[len(writes)-1].confirms(payloadHash(topologyPath, topologyNVLink)),
			"once slurmctld read A, the request may be withdrawn")
	})

	t.Run("ReconfigurePerformed names the generation the payload was rendered from", func(t *testing.T) {
		g := newAggregatedGroup(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		markApplied(t, g, topologyName, topologyNVLink, topologyPath)
		requestReconfigure(t, g)
		g.publishTopology(t, topologyUnknown)
		renderedGeneration := g.get(t, topologyName).Generation

		// The spec moves on once the payload is rendered, and the status patch after the write hands
		// the newer generation back to the reconciliation.
		g.expectWritesWithTopology(t, topologyUnknown).stage.
			Run(func(mock.Arguments) { bumpGeneration(t, g.client, topologyName) })
		prepareSlurmApi(g.slurmapi, g.clock)

		g.reconcile(t, topologyName)

		topology := g.get(t, topologyName)
		require.Equal(t, renderedGeneration+1, topology.Generation)
		condition := meta.FindStatusCondition(topology.Status.Conditions, string(slurmv1alpha1.ReconfigurePerformed))
		require.NotNil(t, condition)
		assert.Equal(t, renderedGeneration, condition.ObservedGeneration,
			"the reconfigure read the spec before the bump, so it must not confirm the bumped one")
		assert.Equal(t, payloadHash(topologyPath, topologyUnknown), topology.Status.AppliedHash)
	})
}

// confirms is the topology controller's check for withdrawing its request, run against the live
// status of the topology config: a reconfigure performed for the current generation over the
// content with this hash.
func (g *aggregatedGroup) confirms(t *testing.T, hash string) bool {
	t.Helper()
	jailedConfig := g.get(t, topologyName)
	return statusWrite{generation: jailedConfig.Generation, status: jailedConfig.Status}.confirms(hash)
}

// TestAggregatedChangedContentRevokesReconfigurePerformed pins that a confirmation holds only while
// the file holds the content it names: it is revoked before other content replaces the file, so a
// topology returning to the confirmed content meanwhile cannot take the old confirmation for a new one.
func TestAggregatedChangedContentRevokesReconfigurePerformed(t *testing.T) {
	t.Run("a topology returning to A while the next one is reconfigured keeps its request", func(t *testing.T) {
		g := newAggregatedGroup(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		// A reconfigure over A ran for the standing request, and the topology fell back to unknown
		// before it finished: the request stays at the generation A was confirmed for.
		requestReconfigure(t, g)
		markApplied(t, g, topologyName, topologyNVLink, topologyPath)
		g.publishTopology(t, topologyUnknown)
		hashNVLink := payloadHash(topologyPath, topologyNVLink)

		g.expectWritesWithTopology(t, topologyUnknown).rename.
			Run(func(mock.Arguments) {
				assert.False(t, g.confirms(t, hashNVLink),
					"the file is about to hold unknown, yet the status confirms A")
			})
		prepareSlurmApi(g.slurmapi, g.clock).
			Run(func(mock.Arguments) {
				g.publishTopology(t, topologyNVLink)
				assert.False(t, g.confirms(t, hashNVLink),
					"slurmctld re-reads unknown, yet the status confirms A")
			})

		g.reconcile(t, topologyName)

		assert.False(t, g.confirms(t, hashNVLink), "slurmctld read unknown, not A")

		// The request still stands, so the next pass writes A and reconfigures again.
		g.nextPass(t)
		g.expectWritesWithTopology(t, topologyNVLink)
		prepareSlurmApi(g.slurmapi, g.clock)

		g.reconcile(t, topologyName)

		assert.True(t, g.confirms(t, hashNVLink), "once slurmctld read A, the request may be withdrawn")
	})

	t.Run("a change without a reconfigure leaves the confirmation revoked", func(t *testing.T) {
		g := newAggregatedGroup(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		markApplied(t, g, topologyName, topologyNVLink, topologyPath)
		generation := g.get(t, topologyName).Generation
		g.publishTopology(t, topologyUnknown)
		g.expectWritesWithTopology(t, topologyUnknown)

		g.reconcile(t, topologyName)

		topology := g.get(t, topologyName)
		condition := meta.FindStatusCondition(topology.Status.Conditions, string(slurmv1alpha1.ReconfigurePerformed))
		require.NotNil(t, condition)
		assert.Equal(t, metav1.ConditionFalse, condition.Status, "slurmctld never re-read unknown")
		assert.Equal(t, generation, condition.ObservedGeneration)
		assert.Equal(t, payloadHash(topologyPath, topologyUnknown), topology.Status.AppliedHash)

		slurmConfigs := meta.FindStatusCondition(g.get(t, slurmConfigsName).Status.Conditions, string(slurmv1alpha1.ReconfigurePerformed))
		require.NotNil(t, slurmConfigs)
		assert.Equal(t, metav1.ConditionTrue, slurmConfigs.Status, "the slurm configs did not change, so their confirmation holds")
	})

	t.Run("a config never confirmed is not given ReconfigurePerformed", func(t *testing.T) {
		g := newAggregatedGroup(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		g.expectWrites(t)

		g.reconcile(t, topologyName)

		assert.Nil(t, meta.FindStatusCondition(g.get(t, topologyName).Status.Conditions, string(slurmv1alpha1.ReconfigurePerformed)))
	})

	t.Run("unchanged content keeps the confirmation", func(t *testing.T) {
		g := newAggregatedGroup(t)
		markApplied(t, g, slurmConfigsName, slurmData, slurmConfPath)
		markApplied(t, g, topologyName, topologyData, topologyPath)
		g.expectWrites(t)

		g.reconcile(t, topologyName)

		require.NotEmpty(t, g.writes[topologyName])
		for _, write := range g.writes[topologyName] {
			condition := write.reconfigurePerformed()
			require.NotNil(t, condition)
			assert.Equal(t, metav1.ConditionTrue, condition.Status, "the file never changed: %+v", write)
		}
	})
}
