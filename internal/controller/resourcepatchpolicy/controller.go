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

// Package resourcepatchpolicy contains the controller that validates
// ResourcePatchPolicy objects and reports their acceptance status. The actual
// application of patches happens in the parent controllers (cluster, nodeset,
// nodeconfigurator) during their reconciliation.
package resourcepatchpolicy

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/resourcepatch"
)

const ControllerName = "resourcepatchpolicy"

// ResourcePatchPolicyReconciler validates ResourcePatchPolicy objects and
// records their acceptance condition.
type ResourcePatchPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=resourcepatchpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=resourcepatchpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=resourcepatchpolicies/finalizers,verbs=update

func (r *ResourcePatchPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	policy := &slurmv1alpha1.ResourcePatchPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !policy.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	condition := metav1.Condition{
		Type:               slurmv1alpha1.ConditionTypeResourcePatchPolicyAccepted,
		ObservedGeneration: policy.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if err := resourcepatch.ValidatePolicy(policy); err != nil {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "Invalid"
		condition.Message = err.Error()
		logger.Info("ResourcePatchPolicy rejected", "reason", err.Error())
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "Accepted"
		condition.Message = "Policy passed static validation"
	}

	if !meta.SetStatusCondition(&policy.Status.Conditions, condition) {
		// No change in the condition; nothing to persist.
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, policy); err != nil {
		// The policy may have been deleted between the cached Get and this
		// update; that is not an error worth retrying.
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to update ResourcePatchPolicy status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourcePatchPolicyReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&slurmv1alpha1.ResourcePatchPolicy{}).
		Named(ControllerName).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}
