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

package reconciler

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/resourcepatch"
)

// ApplyResourcePatchPolicies mutates desired in place by applying every
// ResourcePatchPolicy whose targetRef points at owner. It is a no-op unless the
// feature is enabled on the Reconciler.
//
// Patches are applied to the in-memory desired object before it is submitted to
// the API server, so the operator's reconciliation loop naturally re-applies
// them on every pass. Failed patch entries are logged and skipped; they never
// abort reconciliation of the resource.
func (r Reconciler) ApplyResourcePatchPolicies(
	ctx context.Context,
	owner client.Object,
	desired client.Object,
) {
	if !r.EnableResourcePatchPolicy {
		return
	}

	logger := log.FromContext(ctx)

	ownerGVK, err := apiutil.GVKForObject(owner, r.Scheme)
	if err != nil {
		logger.Error(err, "Failed to resolve owner GVK for ResourcePatchPolicy")
		return
	}

	var list slurmv1alpha1.ResourcePatchPolicyList
	if err := r.List(ctx, &list, client.InNamespace(owner.GetNamespace())); err != nil {
		logger.Error(err, "Failed to list ResourcePatchPolicy objects")
		return
	}
	if len(list.Items) == 0 {
		return
	}

	matching := resourcepatch.FilterPoliciesForTarget(
		list.Items,
		ownerGVK.Group,
		ownerGVK.Kind,
		owner.GetName(),
		owner.GetNamespace(),
	)
	if len(matching) == 0 {
		return
	}

	results, err := resourcepatch.Apply(r.Scheme, desired, matching)
	if err != nil {
		logger.Error(err, "Failed to apply ResourcePatchPolicy", logfield.ResourceKV(desired)...)
		return
	}

	for _, res := range results {
		if res.Applied {
			logger.V(1).Info("Applied ResourcePatchPolicy",
				"policy", res.PolicyName,
				"resourceKind", res.Resource.Kind,
				"resourceName", res.Resource.Name,
			)
			continue
		}
		logger.Info("Skipped ResourcePatchPolicy patch",
			"policy", res.PolicyName,
			"resourceKind", res.Resource.Kind,
			"resourceName", res.Resource.Name,
			"reason", res.Message,
		)
		if r.Recorder != nil {
			r.Recorder.Eventf(owner, "Warning", "ResourcePatchSkipped",
				"Patch from policy %q on %s/%s skipped: %s",
				res.PolicyName, res.Resource.Kind, res.Resource.Name, res.Message)
		}
	}
}
