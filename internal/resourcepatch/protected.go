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
	"reflect"
)

// protectedFieldViolation compares the pre-patch and post-patch JSON documents
// and returns a non-empty message if the patch mutated a field the operator
// relies on for correct reconciliation. An empty string means the patch is
// safe to apply.
//
// Rejected (these break the operator's contract with the API server):
//   - metadata.name / metadata.namespace — identity cannot change
//   - metadata.ownerReferences — breaks garbage collection
//   - spec.selector — immutable after creation on workload resources
func protectedFieldViolation(original, patched []byte) string {
	var before, after genericObject
	if err := json.Unmarshal(original, &before); err != nil {
		return fmt.Sprintf("decoding original object for protected-field check: %v", err)
	}
	if err := json.Unmarshal(patched, &after); err != nil {
		return fmt.Sprintf("decoding patched object for protected-field check: %v", err)
	}

	if before.Metadata.Name != after.Metadata.Name {
		return fmt.Sprintf("patch must not change metadata.name (%q -> %q)",
			before.Metadata.Name, after.Metadata.Name)
	}
	if before.Metadata.Namespace != after.Metadata.Namespace {
		return fmt.Sprintf("patch must not change metadata.namespace (%q -> %q)",
			before.Metadata.Namespace, after.Metadata.Namespace)
	}
	if !reflect.DeepEqual(before.Metadata.OwnerReferences, after.Metadata.OwnerReferences) {
		return "patch must not change metadata.ownerReferences"
	}
	if !reflect.DeepEqual(before.Spec.Selector, after.Spec.Selector) {
		return "patch must not change spec.selector"
	}
	return ""
}

// genericObject captures only the fields relevant to protected-field checks,
// independent of the concrete Kubernetes type. Unmodelled fields are ignored.
// The comparable fields are decoded into interface{} so that comparison is
// insensitive to JSON key ordering produced by the patch library.
type genericObject struct {
	Metadata struct {
		Name            string `json:"name"`
		Namespace       string `json:"namespace"`
		OwnerReferences any    `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		// Selector is present on StatefulSet, Deployment, DaemonSet, etc. and is
		// immutable after creation. nil for resources without a selector.
		Selector any `json:"selector"`
	} `json:"spec"`
}
