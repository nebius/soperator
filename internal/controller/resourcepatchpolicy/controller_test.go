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

package resourcepatchpolicy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func newReconciler(t *testing.T, objs ...client.Object) *ResourcePatchPolicyReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&slurmv1alpha1.ResourcePatchPolicy{}).
		Build()
	return &ResourcePatchPolicyReconciler{Client: c, Scheme: scheme}
}

func basePolicy() *slurmv1alpha1.ResourcePatchPolicy {
	return &slurmv1alpha1.ResourcePatchPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: slurmv1alpha1.ResourcePatchPolicySpec{
			TargetRef: slurmv1alpha1.PolicyTargetReference{Group: "slurm.nebius.ai", Kind: "SlurmCluster", Name: "c"},
			Type:      slurmv1alpha1.JSONPatchType,
			Patches: []slurmv1alpha1.ResourcePatch{{
				ResourceRef: slurmv1alpha1.ResourceSelector{Kind: "StatefulSet"},
				JSONPatch: []slurmv1alpha1.JSONPatchOperation{
					{Op: "add", Path: "/metadata/annotations/a", Value: &apiextensionsv1.JSON{Raw: []byte(`"b"`)}},
				},
			}},
		},
	}
}

func TestReconcile_AcceptsValidPolicy(t *testing.T) {
	policy := basePolicy()
	r := newReconciler(t, policy)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "p"},
	})
	require.NoError(t, err)

	var got slurmv1alpha1.ResourcePatchPolicy
	require.NoError(t, r.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "p"}, &got))
	cond := meta.FindStatusCondition(got.Status.Conditions, slurmv1alpha1.ConditionTypeResourcePatchPolicyAccepted)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, "Accepted", cond.Reason)
}

func TestReconcile_RejectsInvalidPolicy(t *testing.T) {
	policy := basePolicy()
	// Make it invalid: JSONPatch type but no operations.
	policy.Spec.Patches[0].JSONPatch = nil
	r := newReconciler(t, policy)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "p"},
	})
	require.NoError(t, err)

	var got slurmv1alpha1.ResourcePatchPolicy
	require.NoError(t, r.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "p"}, &got))
	cond := meta.FindStatusCondition(got.Status.Conditions, slurmv1alpha1.ConditionTypeResourcePatchPolicyAccepted)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "Invalid", cond.Reason)
	assert.NotEmpty(t, cond.Message)
}

func TestReconcile_NotFoundIsNoError(t *testing.T) {
	r := newReconciler(t)
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "missing"},
	})
	assert.NoError(t, err)
}
