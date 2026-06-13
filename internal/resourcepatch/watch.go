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

package resourcepatch

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

// MapPolicyToTarget returns a handler.MapFunc that, given a ResourcePatchPolicy
// targeting a resource of kind targetKind, enqueues a reconcile request for the
// target resource. It is used to re-reconcile the parent (SlurmCluster,
// NodeSet, NodeConfigurator) whenever a policy that affects it changes.
func MapPolicyToTarget(targetKind string) handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		policy, ok := obj.(*slurmv1alpha1.ResourcePatchPolicy)
		if !ok {
			return nil
		}
		ref := policy.Spec.TargetRef
		if ref.Kind != targetKind {
			return nil
		}

		namespace := policy.Namespace
		if ref.Namespace != nil && *ref.Namespace != "" {
			namespace = *ref.Namespace
		}

		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: namespace,
					Name:      ref.Name,
				},
			},
		}
	}
}
