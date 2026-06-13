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
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch/v5"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

// ValidatePolicy performs static validation of a ResourcePatchPolicy that does
// not depend on the resources being targeted: it checks that every patch entry
// carries a payload consistent with the policy type and that the payload is
// syntactically valid. It returns nil when the policy is acceptable.
func ValidatePolicy(policy *slurmv1alpha1.ResourcePatchPolicy) error {
	if len(policy.Spec.Patches) == 0 {
		return fmt.Errorf("policy must define at least one patch")
	}

	for i := range policy.Spec.Patches {
		patch := &policy.Spec.Patches[i]
		if patch.ResourceRef.Kind == "" {
			return fmt.Errorf("patches[%d]: resourceRef.kind is required", i)
		}

		switch policy.Spec.Type {
		case slurmv1alpha1.JSONPatchType:
			if len(patch.JSONPatch) == 0 {
				return fmt.Errorf("patches[%d]: type is JSONPatch but jsonPatch is empty", i)
			}
			if patch.JSONMergePatch != nil {
				return fmt.Errorf("patches[%d]: jsonMergePatch must not be set when type is JSONPatch", i)
			}
			raw, err := json.Marshal(patch.JSONPatch)
			if err != nil {
				return fmt.Errorf("patches[%d]: marshalling jsonPatch: %w", i, err)
			}
			if _, err := jsonpatch.DecodePatch(raw); err != nil {
				return fmt.Errorf("patches[%d]: invalid jsonPatch: %w", i, err)
			}
			for j := range patch.JSONPatch {
				if err := validateOperation(&patch.JSONPatch[j]); err != nil {
					return fmt.Errorf("patches[%d].jsonPatch[%d]: %w", i, j, err)
				}
			}

		case slurmv1alpha1.JSONMergePatchType:
			if patch.JSONMergePatch == nil || len(patch.JSONMergePatch.Raw) == 0 {
				return fmt.Errorf("patches[%d]: type is JSONMergePatch but jsonMergePatch is empty", i)
			}
			if len(patch.JSONPatch) > 0 {
				return fmt.Errorf("patches[%d]: jsonPatch must not be set when type is JSONMergePatch", i)
			}
			if !json.Valid(patch.JSONMergePatch.Raw) {
				return fmt.Errorf("patches[%d]: jsonMergePatch is not valid JSON", i)
			}

		default:
			return fmt.Errorf("unsupported patch type %q", policy.Spec.Type)
		}
	}
	return nil
}

func validateOperation(op *slurmv1alpha1.JSONPatchOperation) error {
	switch op.Op {
	case "add", "replace", "test":
		if op.Value == nil {
			return fmt.Errorf("op %q requires a value", op.Op)
		}
	case "remove":
		// no value or from required
	case "move", "copy":
		if op.From == nil || *op.From == "" {
			return fmt.Errorf("op %q requires a from path", op.Op)
		}
	default:
		return fmt.Errorf("unknown op %q", op.Op)
	}
	return nil
}
