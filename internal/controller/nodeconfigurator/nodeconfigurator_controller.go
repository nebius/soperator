/*
Copyright 2024 Nebius B.V.

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

package nodeconfigurator

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"

	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/logfield"
	render "nebius.ai/slurm-operator/internal/render/nodeconfigurator"
)

// NodeConfiguratorReconciler reconciles a NodeConfigurator object
type NodeConfiguratorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodeconfigurators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodeconfigurators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodeconfigurators/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *NodeConfiguratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		logfield.ClusterNamespace, req.Namespace,
		logfield.ResourceKind, slurmv1alpha1.KindNodeConfigurator,
		logfield.ResourceName, req.Name,
	)
	log.IntoContext(ctx, logger)

	nodeConfigurator := &slurmv1alpha1.NodeConfigurator{}

	if err := r.Get(ctx, req.NamespacedName, nodeConfigurator); err != nil {
		logger.Error(err, "Failed to get NodeConfigurator")
		// We'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !nodeConfigurator.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	existing := &appsv1.DaemonSet{}
	desired := render.RenderDaemonSet(nodeConfigurator, req.Namespace)

	err := r.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      desired.GetName(),
	}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to get existing DaemonSet")
			return ctrl.Result{}, err
		}
	}

	if err := r.EnsureResourceDeployed(ctx, nodeConfigurator, existing, desired); err != nil {
		logger.Error(err, "Failed to ensure DaemonSet is deployed")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *NodeConfiguratorReconciler) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	return r.Client.Get(ctx, key, obj)
}

func (r *NodeConfiguratorReconciler) Create(ctx context.Context, obj client.Object) error {
	return r.Client.Create(ctx, obj)
}

func (r *NodeConfiguratorReconciler) Update(ctx context.Context, obj client.Object) error {
	return r.Client.Update(ctx, obj)
}

func (r *NodeConfiguratorReconciler) Patch(ctx context.Context, existing, desired client.Object) error {
	return r.Client.Patch(ctx, existing, r.patch(existing, desired))
}

func (r *NodeConfiguratorReconciler) patch(existing, desired client.Object) client.Patch {
	patchImpl := func(dst, src *appsv1.DaemonSet) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		dst.Spec.Template.Spec = src.Spec.Template.Spec

		return res
	}
	return patchImpl(existing.(*appsv1.DaemonSet), desired.(*appsv1.DaemonSet))
}

// EnsureResourceDeployed ensures that `desired` resource is deployed into `owner`. If a corresponding resource `existing` is
// found, it doesn't take any action
func (r *NodeConfiguratorReconciler) EnsureResourceDeployed(
	ctx context.Context,
	owner,
	existing,
	desired client.Object,
) error {
	logger := log.FromContext(ctx).WithValues(logfield.ResourceKV(desired)...)
	logger.V(1).Info("Ensuring resource is deployed")
	logger.V(1).Info("existing", "existing", existing)

	if err := ctrl.SetControllerReference(owner, desired, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference")
		return err
	}

	if existing.GetName() == "" {
		if err := r.Create(ctx, desired); err != nil {
			logger.Error(err, "Failed to create resource")
			return err
		}
		return nil
	}

	logger.V(1).Info("Patching existing resource")
	if err := r.Patch(ctx, existing, desired); err != nil {
		logger.Error(err, "Failed to patch resource")
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeConfiguratorReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&slurmv1alpha1.NodeConfigurator{}).
		Owns(&appsv1.DaemonSet{}).
		Named("nodeconfigurator").
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}
