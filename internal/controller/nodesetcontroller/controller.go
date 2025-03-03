/*
Copyright 2025 Nebius B.V.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nodesetcontroller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

// NodeSetReconciler reconciles a NodeSet object
type NodeSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodesets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodesets/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NodeSet object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/reconcile
func (r *NodeSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeSetReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {
	if err := r.setupConfigMapIndexer(mgr); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("nodeset").
		For(
			&slurmv1alpha1.NodeSet{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		WithOptions(
			controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout),
		).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForConfigMap),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}
