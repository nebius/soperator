package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func concurrentPowerStateClient(t *testing.T, current *slurmv1alpha1.NodeSetPowerState, observations []slurmv1alpha1.NodeSet) (ctrlclient.WithWatch, *atomic.Int32) {
	t.Helper()
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/nodesetpowerstates/worker"):
			gets.Add(1)
			_ = json.NewEncoder(w).Encode(current)
		case strings.HasSuffix(r.URL.Path, "/nodesets"):
			if r.URL.Query().Get("fieldSelector") != "metadata.name=worker" {
				http.Error(w, "missing name selector", http.StatusBadRequest)
				return
			}
			if r.URL.Query().Get("sendInitialEvents") == "true" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(&metav1.Status{TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"}, Status: "Failure", Reason: metav1.StatusReasonBadRequest, Code: http.StatusBadRequest})
				return
			}
			if r.URL.Query().Get("watch") == "true" {
				for i := 1; i < len(observations); i++ {
					next := observations[i].DeepCopy()
					next.ResourceVersion = strconv.Itoa(i + 1)
					_ = json.NewEncoder(w).Encode(map[string]any{"type": "MODIFIED", "object": next})
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			_ = json.NewEncoder(w).Encode(&slurmv1alpha1.NodeSetList{
				TypeMeta: metav1.TypeMeta{APIVersion: slurmv1alpha1.GroupVersion.String(), Kind: "NodeSetList"},
				ListMeta: metav1.ListMeta{ResourceVersion: "1"}, Items: observations[:1],
			})
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{slurmv1alpha1.GroupVersion})
	mapper.Add(slurmv1alpha1.GroupVersion.WithKind("NodeSet"), meta.RESTScopeNamespace)
	mapper.Add(slurmv1alpha1.GroupVersion.WithKind("NodeSetPowerState"), meta.RESTScopeNamespace)
	c, err := ctrlclient.NewWithWatch(&rest.Config{Host: server.URL}, ctrlclient.Options{Scheme: scheme, Mapper: mapper})
	require.NoError(t, err)
	return c, &gets
}

func readyPowerObservation(generation int64, active []int32) slurmv1alpha1.NodeSet {
	return slurmv1alpha1.NodeSet{
		TypeMeta:   metav1.TypeMeta{APIVersion: slurmv1alpha1.GroupVersion.String(), Kind: "NodeSet"},
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test", ResourceVersion: "1", Generation: 2},
		Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: ptr.To(true)},
		Status: slurmv1alpha1.NodeSetStatus{
			AppliedPowerState: &slurmv1alpha1.AppliedPowerState{UID: "power", Generation: generation, ActiveNodes: active},
			Conditions: []metav1.Condition{{
				Type: slurmv1alpha1.ConditionNodeSetPowerReady, Status: metav1.ConditionTrue, ObservedGeneration: 2,
			}},
		},
	}
}

func TestWatchPowerStateConcurrentActions(t *testing.T) {
	for _, tt := range []struct {
		name              string
		added             bool
		appliedGeneration int64
		appliedActive     []int32
		currentGeneration int64
		currentActive     []int32
		replaced          bool
		wantErr           string
	}{
		{name: "unrelated resume after target", added: true, appliedGeneration: 4, appliedActive: []int32{1}, currentGeneration: 5, currentActive: []int32{1, 2}},
		{name: "unrelated resume after newer acknowledgement", added: true, appliedGeneration: 5, appliedActive: []int32{1, 2}, currentGeneration: 6, currentActive: []int32{1, 2, 3}},
		{name: "unrelated action after suspend", appliedGeneration: 5, appliedActive: []int32{}, currentGeneration: 6, currentActive: []int32{2}},
		{name: "resume not in applied snapshot", added: true, appliedGeneration: 5, appliedActive: []int32{2}, currentGeneration: 6, currentActive: []int32{1, 2}, wantErr: "context deadline exceeded"},
		{name: "suspend not in applied snapshot", appliedGeneration: 5, appliedActive: []int32{1}, currentGeneration: 6, wantErr: "context deadline exceeded"},
		{name: "opposite action supersedes resume", added: true, appliedGeneration: 5, appliedActive: []int32{1}, currentGeneration: 6, wantErr: "superseded"},
		{name: "opposite action supersedes suspend", appliedGeneration: 5, appliedActive: []int32{}, currentGeneration: 6, currentActive: []int32{1}, wantErr: "superseded"},
		{name: "replacement", added: true, appliedGeneration: 5, appliedActive: []int32{1}, currentGeneration: 6, currentActive: []int32{1}, replaced: true, wantErr: "replaced"},
		{name: "legacy exact target", added: true, appliedGeneration: 4, currentGeneration: 5, currentActive: []int32{1, 2}},
		{name: "legacy exact live generation", added: true, appliedGeneration: 5, currentGeneration: 5, currentActive: []int32{1, 2}},
		{name: "legacy ambiguous generation", added: true, appliedGeneration: 5, currentGeneration: 6, currentActive: []int32{1, 2}, wantErr: "context deadline exceeded"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			target := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{UID: "power", Generation: 4}}
			if tt.added {
				target.Spec.ActiveNodes = []int32{1}
			}
			current := target.DeepCopy()
			current.Generation = tt.currentGeneration
			current.Spec.ActiveNodes = tt.currentActive
			if tt.replaced {
				current.UID = "replacement"
			}
			observation := readyPowerObservation(tt.appliedGeneration, tt.appliedActive)
			var observations []slurmv1alpha1.NodeSet
			for range 4 {
				observations = append(observations, observation)
			}
			c, gets := concurrentPowerStateClient(t, current, observations)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := watchPowerState(ctx, c, "test", "worker", target, []int32{1}, tt.added)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, int32(1), gets.Load(), "unchanged ready observations must not repeat the live GET")
		})
	}
}

func TestConcurrentWaitersReadDesiredStateOnlyWhenReady(t *testing.T) {
	target := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{UID: "power", Generation: 4}}
	target.Spec.ActiveNodes = []int32{1}
	current := target.DeepCopy()
	current.Generation = 6
	current.Spec.ActiveNodes = []int32{1, 2}
	var observations []slurmv1alpha1.NodeSet
	for range 5 {
		pending := readyPowerObservation(5, []int32{1})
		pending.Status.Conditions[0].Status = metav1.ConditionFalse
		observations = append(observations, pending)
	}
	observations = append(observations, readyPowerObservation(5, []int32{1}))
	c, gets := concurrentPowerStateClient(t, current, observations)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)
	const waiters = 3
	for range waiters {
		group.Go(func() error {
			return watchPowerState(ctx, c, "test", "worker", target, []int32{1}, true)
		})
	}
	require.NoError(t, group.Wait())
	require.Equal(t, int32(waiters), gets.Load(), "pending status events must not trigger live GETs")
}

func TestWatchPowerStateWaitsForMatchingAppliedSnapshot(t *testing.T) {
	target := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{UID: "power", Generation: 4}}
	target.Spec.ActiveNodes = []int32{1}
	current := target.DeepCopy()
	current.Generation = 6
	c, gets := concurrentPowerStateClient(t, current, []slurmv1alpha1.NodeSet{
		readyPowerObservation(5, []int32{}),
		readyPowerObservation(6, []int32{1}),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, watchPowerState(ctx, c, "test", "worker", target, []int32{1}, true))
	require.Equal(t, int32(2), gets.Load(), "readiness for an intermediate opposite action must not complete the wait")
}
