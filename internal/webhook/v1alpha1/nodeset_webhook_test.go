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

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	. "nebius.ai/slurm-operator/internal/webhook/v1alpha1"
)

const (
	testNamespace   = "default"
	testNodeSetName = "test-nodeset"
	testClusterName = "test-cluster"
)

// --- Validator tests ---

func TestNodeSetValidator_ValidateCreate(t *testing.T) {
	validator := &NodeSetCustomValidator{}

	t.Run("accepts NodeSet with slurmClusterRefName set", func(t *testing.T) {
		obj := nodeSetWithClusterRef(testClusterName)
		_, err := validator.ValidateCreate(context.Background(), obj)
		assert.NoError(t, err)
	})

	t.Run("rejects NodeSet without slurmClusterRefName", func(t *testing.T) {
		obj := nodeSetWithoutClusterRef()
		_, err := validator.ValidateCreate(context.Background(), obj)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "slurmClusterRefName")
	})
}

func TestNodeSetValidator_ValidateUpdate(t *testing.T) {
	validator := &NodeSetCustomValidator{}

	t.Run("accepts update that keeps slurmClusterRefName", func(t *testing.T) {
		old := nodeSetWithClusterRef(testClusterName)
		updated := nodeSetWithClusterRef(testClusterName)
		_, err := validator.ValidateUpdate(context.Background(), old, updated)
		assert.NoError(t, err)
	})

	t.Run("rejects update that clears slurmClusterRefName", func(t *testing.T) {
		old := nodeSetWithClusterRef(testClusterName)
		updated := nodeSetWithoutClusterRef()
		_, err := validator.ValidateUpdate(context.Background(), old, updated)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "slurmClusterRefName")
	})
}

func TestNodeSetValidator_ValidateDelete(t *testing.T) {
	validator := &NodeSetCustomValidator{}

	t.Run("always accepts delete", func(t *testing.T) {
		_, err := validator.ValidateDelete(context.Background(), nodeSetWithoutClusterRef())
		assert.NoError(t, err)
	})
}

// --- helpers ---

func nodeSetWithClusterRef(clusterName string) *slurmv1alpha1.NodeSet {
	return &slurmv1alpha1.NodeSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testNodeSetName,
		},
		Spec: slurmv1alpha1.NodeSetSpec{
			SlurmClusterRefName: clusterName,
		},
	}
}

func nodeSetWithoutClusterRef() *slurmv1alpha1.NodeSet {
	return &slurmv1alpha1.NodeSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testNodeSetName,
		},
	}
}
