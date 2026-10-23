package slurmapi_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestClientSetRemoveClientCancelsContextAndDeletesEntry(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clientSet := slurmapi.NewClientSet(ctx)
	clusterName := types.NamespacedName{Namespace: "soperator", Name: "cluster-a"}

	mockClient := slurmapifake.NewMockClient(t)
	mockClient.EXPECT().
		ListNodes(mock.Anything).
		RunAndReturn(func(reqCtx context.Context) ([]slurmapi.Node, error) {
			<-reqCtx.Done()
			return nil, errors.New("request cancelled")
		}).
		Maybe()

	clientSet.AddClient(clusterName, mockClient)

	gotClient, found := clientSet.GetClient(clusterName)
	require.True(t, found)
	require.Equal(t, mockClient, gotClient)

	cache := clientSet.EnsureNodeCache(clusterName, time.Hour, logr.Discard())
	require.NotNil(t, cache)

	waitErrCh := make(chan error, 1)
	go func() {
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer waitCancel()
		waitErrCh <- cache.WaitReady(waitCtx)
	}()

	clientSet.RemoveClient(clusterName)

	_, found = clientSet.GetClient(clusterName)
	require.False(t, found)
	_, found = clientSet.GetNodeCache(clusterName)
	require.False(t, found)

	select {
	case err := <-waitErrCh:
		require.ErrorContains(t, err, "node cache stopped")
	case <-time.After(2 * time.Second):
		t.Fatal("WaitReady did not unblock when client cache was removed")
	}
}
