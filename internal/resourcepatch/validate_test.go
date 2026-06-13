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

package resourcepatch_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/resourcepatch"
)

func mkPolicy(patchType slurmv1alpha1.PatchType, patches ...slurmv1alpha1.ResourcePatch) *slurmv1alpha1.ResourcePatchPolicy {
	return &slurmv1alpha1.ResourcePatchPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: slurmv1alpha1.ResourcePatchPolicySpec{
			TargetRef: slurmv1alpha1.PolicyTargetReference{Group: "slurm.nebius.ai", Kind: "SlurmCluster", Name: "c"},
			Type:      patchType,
			Patches:   patches,
		},
	}
}

func TestValidatePolicy(t *testing.T) {
	val := func(v any) *apiextensionsv1.JSON {
		return jsonValue(t, v)
	}

	tests := []struct {
		name    string
		policy  *slurmv1alpha1.ResourcePatchPolicy
		wantErr bool
	}{
		{
			name: "valid json patch",
			policy: mkPolicy(slurmv1alpha1.JSONPatchType, slurmv1alpha1.ResourcePatch{
				ResourceRef: nameSel("StatefulSet", "x"),
				JSONPatch: []slurmv1alpha1.JSONPatchOperation{
					{Op: "add", Path: "/metadata/annotations/a", Value: val("b")},
				},
			}),
		},
		{
			name: "valid merge patch",
			policy: mkPolicy(slurmv1alpha1.JSONMergePatchType, slurmv1alpha1.ResourcePatch{
				ResourceRef:    nameSel("ConfigMap", "x"),
				JSONMergePatch: &apiextensionsv1.JSON{Raw: []byte(`{"data":{"k":"v"}}`)},
			}),
		},
		{
			name:    "no patches",
			policy:  mkPolicy(slurmv1alpha1.JSONPatchType),
			wantErr: true,
		},
		{
			name: "json patch empty",
			policy: mkPolicy(slurmv1alpha1.JSONPatchType, slurmv1alpha1.ResourcePatch{
				ResourceRef: nameSel("StatefulSet", "x"),
			}),
			wantErr: true,
		},
		{
			name: "json patch with merge payload",
			policy: mkPolicy(slurmv1alpha1.JSONPatchType, slurmv1alpha1.ResourcePatch{
				ResourceRef:    nameSel("StatefulSet", "x"),
				JSONPatch:      []slurmv1alpha1.JSONPatchOperation{{Op: "add", Path: "/a", Value: val("b")}},
				JSONMergePatch: &apiextensionsv1.JSON{Raw: []byte(`{}`)},
			}),
			wantErr: true,
		},
		{
			name: "add without value",
			policy: mkPolicy(slurmv1alpha1.JSONPatchType, slurmv1alpha1.ResourcePatch{
				ResourceRef: nameSel("StatefulSet", "x"),
				JSONPatch:   []slurmv1alpha1.JSONPatchOperation{{Op: "add", Path: "/a"}},
			}),
			wantErr: true,
		},
		{
			name: "move without from",
			policy: mkPolicy(slurmv1alpha1.JSONPatchType, slurmv1alpha1.ResourcePatch{
				ResourceRef: nameSel("StatefulSet", "x"),
				JSONPatch:   []slurmv1alpha1.JSONPatchOperation{{Op: "move", Path: "/a"}},
			}),
			wantErr: true,
		},
		{
			name: "move with from",
			policy: mkPolicy(slurmv1alpha1.JSONPatchType, slurmv1alpha1.ResourcePatch{
				ResourceRef: nameSel("StatefulSet", "x"),
				JSONPatch:   []slurmv1alpha1.JSONPatchOperation{{Op: "move", Path: "/a", From: ptr.To("/b")}},
			}),
		},
		{
			name: "merge patch empty",
			policy: mkPolicy(slurmv1alpha1.JSONMergePatchType, slurmv1alpha1.ResourcePatch{
				ResourceRef: nameSel("ConfigMap", "x"),
			}),
			wantErr: true,
		},
		{
			name: "merge patch invalid json",
			policy: mkPolicy(slurmv1alpha1.JSONMergePatchType, slurmv1alpha1.ResourcePatch{
				ResourceRef:    nameSel("ConfigMap", "x"),
				JSONMergePatch: &apiextensionsv1.JSON{Raw: []byte(`{not json`)},
			}),
			wantErr: true,
		},
		{
			name: "missing resourceRef kind",
			policy: mkPolicy(slurmv1alpha1.JSONPatchType, slurmv1alpha1.ResourcePatch{
				JSONPatch: []slurmv1alpha1.JSONPatchOperation{{Op: "add", Path: "/a", Value: val("b")}},
			}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := resourcepatch.ValidatePolicy(tt.policy)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
