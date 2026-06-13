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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	render "nebius.ai/slurm-operator/internal/render/nodeconfigurator"
)

func ncScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	return scheme
}

func nodeConfiguratorPolicy() *slurmv1alpha1.ResourcePatchPolicy {
	return &slurmv1alpha1.ResourcePatchPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "nc", Namespace: "test-namespace"},
		Spec: slurmv1alpha1.ResourcePatchPolicySpec{
			TargetRef: slurmv1alpha1.PolicyTargetReference{
				Group: slurmv1alpha1.GroupVersion.Group,
				Kind:  slurmv1alpha1.KindNodeConfigurator,
				Name:  "node-configurator",
			},
			Type: slurmv1alpha1.JSONPatchType,
			Patches: []slurmv1alpha1.ResourcePatch{{
				ResourceRef: slurmv1alpha1.ResourceSelector{Kind: "DaemonSet", Name: ptr.To("node-configurator-ds")},
				JSONPatch: []slurmv1alpha1.JSONPatchOperation{
					{Op: "add", Path: "/metadata/labels/team", Value: &apiextensionsv1.JSON{Raw: []byte(`"hpc"`)}},
				},
			}},
		},
	}
}

func TestApplyResourcePatchPolicies_PatchesDaemonSet(t *testing.T) {
	scheme := ncScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(nodeConfiguratorPolicy()).Build()
	r := &NodeConfiguratorReconciler{Client: c, Scheme: scheme, EnableResourcePatchPolicy: true}

	nc := &slurmv1alpha1.NodeConfigurator{
		ObjectMeta: metav1.ObjectMeta{Name: "node-configurator", Namespace: "test-namespace"},
		Spec:       slurmv1alpha1.NodeConfiguratorSpec{Rebooter: slurmv1alpha1.Rebooter{Enabled: true}},
	}
	ds := render.RenderDaemonSet(nc, "test-namespace")

	r.applyResourcePatchPolicies(context.Background(), nc, ds)

	assert.Equal(t, "hpc", ds.Labels["team"])
}

func TestApplyResourcePatchPolicies_DisabledNoOp(t *testing.T) {
	scheme := ncScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(nodeConfiguratorPolicy()).Build()
	r := &NodeConfiguratorReconciler{Client: c, Scheme: scheme, EnableResourcePatchPolicy: false}

	nc := &slurmv1alpha1.NodeConfigurator{
		ObjectMeta: metav1.ObjectMeta{Name: "node-configurator", Namespace: "test-namespace"},
		Spec:       slurmv1alpha1.NodeConfiguratorSpec{Rebooter: slurmv1alpha1.Rebooter{Enabled: true}},
	}
	ds := render.RenderDaemonSet(nc, "test-namespace")

	r.applyResourcePatchPolicies(context.Background(), nc, ds)
	_, ok := ds.Labels["team"]
	assert.False(t, ok)
}
