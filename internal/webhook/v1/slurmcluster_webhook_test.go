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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	. "nebius.ai/slurm-operator/internal/webhook/v1"
)

func TestValidateSlurmClusterCreate(t *testing.T) {
	validator := &SlurmClusterCustomValidator{}

	t.Run("Creation should admit structured partition configuration", func(t *testing.T) {
		obj := &slurmv1.SlurmCluster{
			Spec: slurmv1.SlurmClusterSpec{
				PartitionConfiguration: slurmv1.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeStructured,
				},
			},
		}

		_, err := validator.ValidateCreate(context.Background(), obj)
		assert.NoError(t, err)
	})
}

func TestValidateSlurmClusterUpdate(t *testing.T) {
	validator := &SlurmClusterCustomValidator{}

	t.Run("Update should admit structured partition configuration", func(t *testing.T) {
		oldObj := &slurmv1.SlurmCluster{}
		obj := &slurmv1.SlurmCluster{
			Spec: slurmv1.SlurmClusterSpec{
				PartitionConfiguration: slurmv1.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeStructured,
				},
			},
		}

		_, err := validator.ValidateUpdate(context.Background(), oldObj, obj)
		assert.NoError(t, err)
	})
}

func TestValidateSlurmClusterUserIsolation(t *testing.T) {
	validator := &SlurmClusterCustomValidator{}

	clusterWith := func(isolation *slurmv1.LoginUserIsolation) *slurmv1.SlurmCluster {
		return &slurmv1.SlurmCluster{
			Spec: slurmv1.SlurmClusterSpec{
				SlurmNodes: slurmv1.SlurmNodes{
					Login: slurmv1.SlurmNodeLogin{
						Sshd: slurmv1.NodeContainer{
							Resources: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("9Gi"),
							},
						},
						UserIsolation: isolation,
					},
				},
			},
		}
	}

	t.Run("admits limits below the container memory limit", func(t *testing.T) {
		memoryMax := resource.MustParse("8Gi")
		_, err := validator.ValidateCreate(context.Background(), clusterWith(&slurmv1.LoginUserIsolation{
			Enabled:   ptr.To(true),
			MemoryMax: &memoryMax,
		}))
		assert.NoError(t, err)
	})

	t.Run("admits explicit memoryHigh below derived memoryMax", func(t *testing.T) {
		memoryHigh := resource.MustParse("8Gi")
		_, err := validator.ValidateCreate(context.Background(), clusterWith(&slurmv1.LoginUserIsolation{
			Enabled:    ptr.To(true),
			MemoryHigh: &memoryHigh,
		}))
		assert.NoError(t, err)
	})

	t.Run("rejects explicit memoryMax below derived memoryHigh", func(t *testing.T) {
		memoryMax := resource.MustParse("7Gi")
		_, err := validator.ValidateCreate(context.Background(), clusterWith(&slurmv1.LoginUserIsolation{
			Enabled:   ptr.To(true),
			MemoryMax: &memoryMax,
		}))
		assert.ErrorContains(t, err, "must not exceed memoryMax")
	})

	t.Run("rejects explicit memoryHigh above derived memoryMax", func(t *testing.T) {
		memoryHigh := resource.MustParse("8500Mi")
		_, err := validator.ValidateCreate(context.Background(), clusterWith(&slurmv1.LoginUserIsolation{
			Enabled:    ptr.To(true),
			MemoryHigh: &memoryHigh,
		}))
		assert.ErrorContains(t, err, "must not exceed memoryMax")
	})

	t.Run("rejects memoryMax at or above the container memory limit", func(t *testing.T) {
		memoryMax := resource.MustParse("9Gi")
		_, err := validator.ValidateCreate(context.Background(), clusterWith(&slurmv1.LoginUserIsolation{
			Enabled:   ptr.To(true),
			MemoryMax: &memoryMax,
		}))
		assert.ErrorContains(t, err, "memoryMax")
	})

	t.Run("rejects memoryHigh at or above the container memory limit", func(t *testing.T) {
		memoryHigh := resource.MustParse("10Gi")
		_, err := validator.ValidateCreate(context.Background(), clusterWith(&slurmv1.LoginUserIsolation{
			Enabled:    ptr.To(true),
			MemoryHigh: &memoryHigh,
		}))
		assert.ErrorContains(t, err, "memoryHigh")
	})

	for _, field := range []string{"memoryHigh", "memoryMax"} {
		t.Run("rejects non-positive "+field, func(t *testing.T) {
			quantity := resource.MustParse("-1Gi")
			isolation := &slurmv1.LoginUserIsolation{Enabled: ptr.To(true)}
			switch field {
			case "memoryHigh":
				isolation.MemoryHigh = &quantity
			case "memoryMax":
				isolation.MemoryMax = &quantity
			}

			_, err := validator.ValidateCreate(context.Background(), clusterWith(isolation))
			assert.ErrorContains(t, err, "must be greater than zero")
		})
	}

	t.Run("ignores limits when isolation is disabled", func(t *testing.T) {
		memoryMax := resource.MustParse("100Gi")
		_, err := validator.ValidateCreate(context.Background(), clusterWith(&slurmv1.LoginUserIsolation{
			Enabled:   ptr.To(false),
			MemoryMax: &memoryMax,
		}))
		assert.NoError(t, err)
	})
}

func TestValidateSlurmClusterLoginDocker(t *testing.T) {
	validator := &SlurmClusterCustomValidator{}
	clusterWith := func(
		docker *slurmv1.LoginDocker,
		isolation *slurmv1.LoginUserIsolation,
		mounts ...slurmv1.NodeVolumeMount,
	) *slurmv1.SlurmCluster {
		return &slurmv1.SlurmCluster{
			Spec: slurmv1.SlurmClusterSpec{
				SlurmNodes: slurmv1.SlurmNodes{
					Login: slurmv1.SlurmNodeLogin{
						Docker:        docker,
						UserIsolation: isolation,
						Volumes:       slurmv1.SlurmNodeLoginVolumes{JailSubMounts: mounts},
					},
				},
			},
		}
	}

	enabled := &slurmv1.LoginDocker{Enabled: ptr.To(true)}
	isolation := &slurmv1.LoginUserIsolation{Enabled: ptr.To(true)}

	t.Run("admits omitted Docker without prerequisites", func(t *testing.T) {
		_, err := validator.ValidateCreate(context.Background(), clusterWith(nil, nil))
		assert.NoError(t, err)
	})

	t.Run("admits disabled Docker without prerequisites", func(t *testing.T) {
		_, err := validator.ValidateCreate(context.Background(), clusterWith(
			&slurmv1.LoginDocker{Enabled: ptr.To(false)},
			nil,
		))
		assert.NoError(t, err)
	})

	t.Run("rejects reserved Docker environment", func(t *testing.T) {
		cluster := clusterWith(nil, nil)
		cluster.Spec.SlurmNodes.Login.Sshd.CustomEnv = []corev1.EnvVar{{
			Name:  consts.EnvDockerEnabled,
			Value: "true",
		}}
		_, err := validator.ValidateCreate(context.Background(), cluster)
		assert.ErrorContains(t, err, "managed by Soperator")
	})

	t.Run("rejects enabled Docker without user isolation", func(t *testing.T) {
		_, err := validator.ValidateCreate(context.Background(), clusterWith(enabled, nil))
		assert.ErrorContains(t, err, "userIsolation.enabled=true")
	})

	t.Run("rejects enabled Docker without image storage", func(t *testing.T) {
		_, err := validator.ValidateCreate(context.Background(), clusterWith(enabled, isolation))
		assert.ErrorContains(t, err, consts.ImageStorageMountPath)
	})

	t.Run("rejects read-only image storage", func(t *testing.T) {
		_, err := validator.ValidateCreate(context.Background(), clusterWith(
			enabled,
			isolation,
			slurmv1.NodeVolumeMount{MountPath: consts.ImageStorageMountPath, ReadOnly: true},
		))
		assert.ErrorContains(t, err, "as writable")
	})

	t.Run("admits enabled Docker with prerequisites", func(t *testing.T) {
		_, err := validator.ValidateCreate(context.Background(), clusterWith(
			enabled,
			isolation,
			slurmv1.NodeVolumeMount{MountPath: consts.ImageStorageMountPath},
		))
		assert.NoError(t, err)
	})
}
