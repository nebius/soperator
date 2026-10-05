package controllerconfig

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type controllerNameKey struct{}

// WithControllerName returns a context carrying the name of the controller that is doing the work.
// Shared clients (Kubernetes API, Slurm REST) read it to attribute their requests.
func WithControllerName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, controllerNameKey{}, name)
}

// ControllerNameFromContext returns the controller name stored by WithControllerName, or an empty
// string for work that does not originate from a reconcile: informers, leader election, webhooks, runnables.
func ControllerNameFromContext(ctx context.Context) string {
	name, _ := ctx.Value(controllerNameKey{}).(string)
	return name
}

// NamedReconciler tags the context of every Reconcile call with the controller name, so that
// requests issued through shared clients during the reconcile are attributed to this controller.
// Use it at the builder's Complete with the same name passed to Named.
func NamedReconciler(name string, r reconcile.Reconciler) reconcile.Reconciler {
	return reconcile.Func(func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
		return r.Reconcile(WithControllerName(ctx, name), req)
	})
}
