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

package v1_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/feature"
	. "nebius.ai/slurm-operator/internal/webhook/v1"
)

func TestValidateSlurmClusterCreate(t *testing.T) {
	validator := &SlurmClusterCustomValidator{}

	t.Run("Creation should be denied if NodeSets are disabled but partition configuration is structured", func(t *testing.T) {
		err := feature.Gate.SetFromMap(map[string]bool{
			string(feature.NodeSetWorkers): false,
		})
		assert.NoError(t, err)

		obj := &slurmv1.SlurmCluster{
			Spec: slurmv1.SlurmClusterSpec{
				PartitionConfiguration: slurmv1.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeStructured,
				},
			},
		}

		_, err = validator.ValidateCreate(context.Background(), obj)
		assert.Error(t, err)
	})

	t.Run("Creation should be admit if NodeSets are enabled and partition configuration is structured", func(t *testing.T) {
		err := feature.Gate.SetFromMap(map[string]bool{
			string(feature.NodeSetWorkers): true,
		})
		assert.NoError(t, err)

		obj := &slurmv1.SlurmCluster{
			Spec: slurmv1.SlurmClusterSpec{
				PartitionConfiguration: slurmv1.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeStructured,
				},
			},
		}

		_, err = validator.ValidateCreate(context.Background(), obj)
		assert.NoError(t, err)
	})
}

func TestValidateSlurmClusterUpdate(t *testing.T) {
	validator := &SlurmClusterCustomValidator{}

	t.Run("Update should be denied if NodeSets are disabled but partition configuration is structured", func(t *testing.T) {
		err := feature.Gate.SetFromMap(map[string]bool{
			string(feature.NodeSetWorkers): false,
		})
		assert.NoError(t, err)

		oldObj := &slurmv1.SlurmCluster{}
		obj := &slurmv1.SlurmCluster{
			Spec: slurmv1.SlurmClusterSpec{
				PartitionConfiguration: slurmv1.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeStructured,
				},
			},
		}

		_, err = validator.ValidateUpdate(context.Background(), oldObj, obj)
		assert.Error(t, err)
	})

	t.Run("Update should be admit if NodeSets are enabled and partition configuration is structured", func(t *testing.T) {
		err := feature.Gate.SetFromMap(map[string]bool{
			string(feature.NodeSetWorkers): true,
		})
		assert.NoError(t, err)

		oldObj := &slurmv1.SlurmCluster{}
		obj := &slurmv1.SlurmCluster{
			Spec: slurmv1.SlurmClusterSpec{
				PartitionConfiguration: slurmv1.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeStructured,
				},
			},
		}

		_, err = validator.ValidateUpdate(context.Background(), oldObj, obj)
		assert.NoError(t, err)
	})
}
