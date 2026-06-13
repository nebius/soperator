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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func patchScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	return scheme
}

func clusterPatchPolicy() *slurmv1alpha1.ResourcePatchPolicy {
	return &slurmv1alpha1.ResourcePatchPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: slurmv1alpha1.ResourcePatchPolicySpec{
			TargetRef: slurmv1alpha1.PolicyTargetReference{
				Group: "slurm.nebius.ai", Kind: slurmv1.KindSlurmCluster, Name: "test-cluster",
			},
			Type: slurmv1alpha1.JSONPatchType,
			Patches: []slurmv1alpha1.ResourcePatch{{
				ResourceRef: slurmv1alpha1.ResourceSelector{Kind: "StatefulSet", Name: ptrString("test-cluster-worker")},
				JSONPatch: []slurmv1alpha1.JSONPatchOperation{
					{Op: "add", Path: "/metadata/annotations/patched", Value: &apiextensionsv1.JSON{Raw: []byte(`"yes"`)}},
				},
			}},
		},
	}
}

func ptrString(s string) *string { return &s }

func workerStatefulSet() *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-cluster-worker",
			Namespace:   "default",
			Annotations: map[string]string{"existing": "v"},
		},
	}
}

func TestApplyResourcePatchPolicies_PatchesDesired(t *testing.T) {
	scheme := patchScheme(t)
	cluster := &slurmv1.SlurmCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"}}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(clusterPatchPolicy()).
		Build()

	r := Reconciler{Client: c, Scheme: scheme, EnableResourcePatchPolicy: true}
	sts := workerStatefulSet()

	r.ApplyResourcePatchPolicies(context.Background(), cluster, sts)

	assert.Equal(t, "yes", sts.Annotations["patched"])
	assert.Equal(t, "v", sts.Annotations["existing"])
}

func TestApplyResourcePatchPolicies_DisabledIsNoOp(t *testing.T) {
	scheme := patchScheme(t)
	cluster := &slurmv1.SlurmCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"}}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(clusterPatchPolicy()).
		Build()

	r := Reconciler{Client: c, Scheme: scheme, EnableResourcePatchPolicy: false}
	sts := workerStatefulSet()

	r.ApplyResourcePatchPolicies(context.Background(), cluster, sts)

	_, ok := sts.Annotations["patched"]
	assert.False(t, ok, "feature disabled must not patch")
}

func TestApplyResourcePatchPolicies_NonMatchingTargetIgnored(t *testing.T) {
	scheme := patchScheme(t)
	// Policy targets a different cluster name.
	policy := clusterPatchPolicy()
	policy.Spec.TargetRef.Name = "other-cluster"

	cluster := &slurmv1.SlurmCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy).Build()

	r := Reconciler{Client: c, Scheme: scheme, EnableResourcePatchPolicy: true}
	sts := workerStatefulSet()

	r.ApplyResourcePatchPolicies(context.Background(), cluster, sts)

	_, ok := sts.Annotations["patched"]
	assert.False(t, ok, "policy for a different target must not patch")
}

var _ client.Object = (*slurmv1alpha1.ResourcePatchPolicy)(nil)
