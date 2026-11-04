package controllerconfig

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestControllerNameFromContextDefaultsToEmpty(t *testing.T) {
	require.Empty(t, ControllerNameFromContext(context.Background()))
	require.Equal(t, "cluster", ControllerNameFromContext(WithControllerName(context.Background(), "cluster")))
}

func TestNamedReconcilerTagsContextAndPassesResultThrough(t *testing.T) {
	var seenName string
	var seenReq reconcile.Request
	wantErr := errors.New("boom")
	inner := reconcile.Func(func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
		seenName = ControllerNameFromContext(ctx)
		seenReq = req
		return reconcile.Result{Requeue: true}, wantErr
	})

	req := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "obj"}}
	res, err := NamedReconciler("nodeset", inner).Reconcile(context.Background(), req)

	require.ErrorIs(t, err, wantErr)
	require.True(t, res.Requeue)
	require.Equal(t, "nodeset", seenName)
	require.Equal(t, req, seenReq)
}
