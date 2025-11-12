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

package v1alpha1_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	. "nebius.ai/slurm-operator/internal/webhook/v1alpha1"
)

func TestDefaultSlurmClusterRef(t *testing.T) {
	defaulter := &NodeSetCustomDefaulter{
		Client: newClientBuilder().Build(),
	}
	const (
		testNamespace   = "default"
		testNodeSetName = "test-nodeset"
		testClusterName = "test-cluster"
	)

	t.Run("Parental cluster ref exists", func(t *testing.T) {
		obj := &slurmv1alpha1.NodeSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testNamespace,
				Name:      testNodeSetName,
				Annotations: map[string]string{
					consts.AnnotationParentalClusterRefName: testClusterName,
				},
			},
		}

		err := defaulter.Default(context.Background(), obj)
		assert.NoError(t, err)
	})

	t.Run("Parental cluster ref is set", func(t *testing.T) {
		defer func() {
			defaulter.Client = newClientBuilder().Build()
		}()

		defaulter.Client = newClientBuilder().
			WithObjects(&slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      testClusterName,
				},
			}).
			Build()

		obj := &slurmv1alpha1.NodeSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testNamespace,
				Name:      testClusterName,
			},
		}

		err := defaulter.Default(context.Background(), obj)
		assert.NoError(t, err)
		assert.Equal(t, testClusterName, obj.GetAnnotations()[consts.AnnotationParentalClusterRefName])
	})

	t.Run("Parental cluster doesn't exist", func(t *testing.T) {
		obj := &slurmv1alpha1.NodeSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testNamespace,
				Name:      testClusterName,
			},
		}

		err := defaulter.Default(context.Background(), obj)
		assert.Error(t, err)
	})
}

func newClientBuilder() *fake.ClientBuilder {
	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = slurmv1alpha1.AddToScheme(scheme)
	return fake.NewClientBuilder().
		WithScheme(scheme)
}
