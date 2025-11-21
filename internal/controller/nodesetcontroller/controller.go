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
	errorsStd "errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/logfield"
)

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodesets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodesets/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;update;patch;delete;create
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=create;delete;get;list;patch;update;watch
// +kubebuilder:rbac:groups=core,resources=podtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// NodeSetReconciler reconciles a NodeSet object
type NodeSetReconciler struct {
	*reconciler.Reconciler

	AdvancedStatefulSet *reconciler.AdvancedStatefulSetReconciler
	Service             *reconciler.ServiceReconciler
	Secret              *reconciler.SecretReconciler
	ConfigMap           *reconciler.ConfigMapReconciler
}

func NewNodeSetReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder) *NodeSetReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	return &NodeSetReconciler{
		Reconciler:          r,
		AdvancedStatefulSet: reconciler.NewAdvancedStatefulSetReconciler(r),
		Service:             reconciler.NewServiceReconciler(r),
		Secret:              reconciler.NewSecretReconciler(r),
		ConfigMap:           reconciler.NewConfigMapReconciler(r),
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeSetReconciler) SetupWithManager(mgr ctrl.Manager, name string, maxConcurrency int, cacheSyncTimeout time.Duration) error {
	if err := r.setupConfigMapIndexer(mgr); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
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

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *NodeSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		logfield.ClusterNamespace, req.Namespace,
		logfield.ResourceKind, slurmv1alpha1.KindNodeSet,
		logfield.ResourceName, req.Name,
	)
	log.IntoContext(ctx, logger)

	nodeSet := &slurmv1alpha1.NodeSet{}
	err := r.Get(ctx, req.NamespacedName, nodeSet)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("Resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get resource")
		return ctrl.Result{Requeue: true}, fmt.Errorf("getting %s: %w", slurmv1alpha1.KindNodeSet, err)
	}

	// If nodeset is marked for deletion, we have nothing to do
	if nodeSet.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	result, err := r.reconcile(ctx, nodeSet)
	if err != nil {
		logger.Error(err, "Failed to reconcile resource")
		result = ctrl.Result{}
		err = fmt.Errorf("reconciling %s: %w", slurmv1alpha1.KindNodeSet, err)
	}

	statusErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		innerNodeSet := &slurmv1alpha1.NodeSet{}
		innerErr := r.Get(ctx, req.NamespacedName, innerNodeSet)
		if innerErr != nil {
			if apierrors.IsNotFound(innerErr) {
				logger.V(1).Info("Resource not found. Ignoring since object must be deleted")
				return nil
			}
			// Error reading the object - requeue the request.
			logger.Error(innerErr, "Failed to get resource")
			return fmt.Errorf("getting %s: %w", slurmv1alpha1.KindNodeSet, innerErr)
		}

		return r.Status().Update(ctx, innerNodeSet)
	})
	if statusErr != nil {
		logger.Error(statusErr, "Failed to update resource status")
		result = ctrl.Result{}
		err = fmt.Errorf("updating %s status: %w", slurmv1alpha1.KindNodeSet, statusErr)
	}

	return result, errorsStd.Join(err, statusErr)
}

func (r *NodeSetReconciler) patchStatus(ctx context.Context, obj *slurmv1alpha1.NodeSet, patcher func(status *slurmv1alpha1.NodeSetStatus)) error {
	patch := client.MergeFrom(obj.DeepCopy())
	patcher(&obj.Status)
	if err := r.Status().Patch(ctx, obj, patch); err != nil {
		log.FromContext(ctx).Error(err, "Failed to patch status")
		return fmt.Errorf("patching cluster status: %w", err)
	}
	return nil
}
