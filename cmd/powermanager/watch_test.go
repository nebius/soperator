package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func TestWatchPowerStateRelistsAfterExpiredWatch(t *testing.T) {
	target := &slurmv1alpha1.NodeSetPowerState{
		TypeMeta:   metav1.TypeMeta{APIVersion: slurmv1alpha1.GroupVersion.String(), Kind: "NodeSetPowerState"},
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test", UID: "power", Generation: 4},
		Spec:       slurmv1alpha1.NodeSetPowerStateSpec{ActiveNodes: []int32{1}},
	}
	var lists, watches, gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/nodesetpowerstates/worker"):
			gets.Add(1)
			_ = json.NewEncoder(w).Encode(target)
		case strings.HasSuffix(r.URL.Path, "/nodesets"):
			if r.URL.Query().Get("fieldSelector") != "metadata.name=worker" {
				http.Error(w, "missing name selector", http.StatusBadRequest)
				return
			}
			if r.URL.Query().Get("sendInitialEvents") == "true" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(&metav1.Status{TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"}, Status: "Failure", Reason: metav1.StatusReasonBadRequest, Code: http.StatusBadRequest, Message: "streaming lists unsupported"})
				return
			}
			if r.URL.Query().Get("watch") == "true" {
				watches.Add(1)
				if lists.Load() == 1 {
					_, _ = w.Write([]byte(`{"type":"ERROR","object":{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Expired","code":410,"message":"resource version expired"}}` + "\n"))
					return
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			count := lists.Add(1)
			rv := strconv.Itoa(20 + int(count))
			ns := slurmv1alpha1.NodeSet{
				TypeMeta:   metav1.TypeMeta{APIVersion: slurmv1alpha1.GroupVersion.String(), Kind: "NodeSet"},
				ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test", ResourceVersion: rv, Generation: 2},
				Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: ptr.To(true)},
				Status:     slurmv1alpha1.NodeSetStatus{AppliedPowerState: &slurmv1alpha1.AppliedPowerState{UID: "power", Generation: 4}, Conditions: []metav1.Condition{{Type: slurmv1alpha1.ConditionNodeSetPowerReady, Status: metav1.ConditionFalse, ObservedGeneration: 2}}},
			}
			if count > 1 {
				ns.Status.Conditions[0].Status = metav1.ConditionTrue
			}
			_ = json.NewEncoder(w).Encode(&slurmv1alpha1.NodeSetList{TypeMeta: metav1.TypeMeta{APIVersion: slurmv1alpha1.GroupVersion.String(), Kind: "NodeSetList"}, ListMeta: metav1.ListMeta{ResourceVersion: rv}, Items: []slurmv1alpha1.NodeSet{ns}})
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{slurmv1alpha1.GroupVersion})
	mapper.Add(slurmv1alpha1.GroupVersion.WithKind("NodeSet"), meta.RESTScopeNamespace)
	mapper.Add(slurmv1alpha1.GroupVersion.WithKind("NodeSetPowerState"), meta.RESTScopeNamespace)
	c, err := ctrlclient.NewWithWatch(&rest.Config{Host: server.URL}, ctrlclient.Options{Scheme: scheme, Mapper: mapper})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = watchPowerState(ctx, c, "test", "worker", target, []int32{1}, true)
	require.NoError(t, err)
	require.GreaterOrEqual(t, lists.Load(), int32(2))
	require.GreaterOrEqual(t, watches.Load(), int32(1))
	require.Equal(t, int32(1), gets.Load(), "live desired state is read only when readiness is acknowledged")
}

func TestWatchActiveNodesWaitsForFutureTransition(t *testing.T) {
	for _, added := range []bool{true, false} {
		for _, absent := range []bool{true, false} {
			t.Run(fmt.Sprintf("added=%t/absent=%t", added, absent), func(t *testing.T) {
				state := slurmv1alpha1.NodeSetPowerState{
					TypeMeta:   metav1.TypeMeta{APIVersion: slurmv1alpha1.GroupVersion.String(), Kind: "NodeSetPowerState"},
					ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test", UID: "power", ResourceVersion: "1"},
				}
				if !added {
					state.Spec.ActiveNodes = []int32{1}
				}
				var watches atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if !strings.HasSuffix(r.URL.Path, "/nodesetpowerstates") || r.URL.Query().Get("fieldSelector") != "metadata.name=worker" {
						http.Error(w, "unexpected request", http.StatusBadRequest)
						return
					}
					if r.URL.Query().Get("sendInitialEvents") == "true" {
						w.WriteHeader(http.StatusBadRequest)
						_ = json.NewEncoder(w).Encode(&metav1.Status{TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"}, Status: "Failure", Reason: metav1.StatusReasonBadRequest, Code: http.StatusBadRequest})
						return
					}
					if r.URL.Query().Get("watch") == "true" {
						watches.Add(1)
						next := state.DeepCopy()
						next.ResourceVersion = "2"
						if added {
							next.Spec.ActiveNodes = []int32{1}
						} else {
							next.Spec.ActiveNodes = nil
						}
						eventType := "MODIFIED"
						if absent {
							eventType = "ADDED"
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"type": eventType, "object": next})
						w.(http.Flusher).Flush()
						<-r.Context().Done()
						return
					}
					list := &slurmv1alpha1.NodeSetPowerStateList{TypeMeta: metav1.TypeMeta{APIVersion: slurmv1alpha1.GroupVersion.String(), Kind: "NodeSetPowerStateList"}, ListMeta: metav1.ListMeta{ResourceVersion: "1"}}
					if !absent {
						list.Items = []slurmv1alpha1.NodeSetPowerState{state}
					}
					_ = json.NewEncoder(w).Encode(list)
				}))
				defer server.Close()
				mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{slurmv1alpha1.GroupVersion})
				mapper.Add(slurmv1alpha1.GroupVersion.WithKind("NodeSetPowerState"), meta.RESTScopeNamespace)
				c, err := ctrlclient.NewWithWatch(&rest.Config{Host: server.URL}, ctrlclient.Options{Scheme: scheme, Mapper: mapper})
				require.NoError(t, err)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				require.NoError(t, watchActiveNodes(ctx, c, "test", "worker", []int32{1}, added))
				require.GreaterOrEqual(t, watches.Load(), int32(1))
			})
		}
	}
}
