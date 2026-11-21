package slurmapi_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	"nebius.ai/slurm-operator/internal/slurmapi"
	"nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestClientSet_RemoveClient(t *testing.T) {
	removed := types.NamespacedName{Namespace: "ns", Name: "removed"}
	kept := types.NamespacedName{Namespace: "ns", Name: "kept"}

	clients := slurmapi.NewClientSet(t.Context())
	clients.AddClient(removed, fake.NewMockClient(t))
	clients.AddClient(kept, fake.NewMockClient(t))

	clients.RemoveClient(removed)

	_, found := clients.GetClient(removed)
	require.False(t, found)
	_, found = clients.GetClient(kept)
	require.True(t, found)
	require.Len(t, clients.GetClients(), 1)
	require.Nil(t, clients.EnsureNodeCache(removed, time.Hour, logr.Discard()))

	// Removing a cluster that is not registered must be a no-op.
	clients.RemoveClient(removed)
	require.Len(t, clients.GetClients(), 1)
}

func TestClientSet_RemoveClient_stopsNodeCache(t *testing.T) {
	cluster := types.NamespacedName{Namespace: "ns", Name: "cluster"}

	cancelled := make(chan struct{})
	apiClient := fake.NewMockClient(t)
	apiClient.EXPECT().ListNodes(mock.Anything).RunAndReturn(func(ctx context.Context) ([]slurmapi.Node, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}).Once()

	clients := slurmapi.NewClientSet(t.Context())
	clients.AddClient(cluster, apiClient)
	require.NotNil(t, clients.EnsureNodeCache(cluster, time.Hour, logr.Discard()))

	clients.RemoveClient(cluster)

	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("node cache refresh was not cancelled by RemoveClient")
	}
	_, found := clients.GetNodeCache(cluster)
	require.False(t, found)
}
