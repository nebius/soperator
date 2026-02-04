package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"

	"nebius.ai/slurm-operator/internal/values"
)

func TestCalculateReplicasAndReserveOrdinals(t *testing.T) {
	tests := []struct {
		name                    string
		activeNodes             []int32
		expectedReplicas        int32
		expectedReserveOrdinals []intstr.IntOrString
	}{
		{
			name:                    "empty activeNodes returns zero replicas",
			activeNodes:             []int32{},
			expectedReplicas:        0,
			expectedReserveOrdinals: nil,
		},
		{
			name:                    "nil activeNodes returns zero replicas",
			activeNodes:             nil,
			expectedReplicas:        0,
			expectedReserveOrdinals: nil,
		},
		{
			name:                    "single node at ordinal 0",
			activeNodes:             []int32{0},
			expectedReplicas:        1,
			expectedReserveOrdinals: nil, // No gaps, no reserved ordinals
		},
		{
			name:             "single node at ordinal 5",
			activeNodes:      []int32{5},
			expectedReplicas: 1,
			expectedReserveOrdinals: []intstr.IntOrString{
				intstr.FromInt32(0),
				intstr.FromInt32(1),
				intstr.FromInt32(2),
				intstr.FromInt32(3),
				intstr.FromInt32(4),
			}, // 0-4 are reserved since only node 5 is active
		},
		{
			name:                    "consecutive nodes starting from 0",
			activeNodes:             []int32{0, 1, 2},
			expectedReplicas:        3,
			expectedReserveOrdinals: nil, // No gaps
		},
		{
			name:             "nodes with gaps",
			activeNodes:      []int32{0, 3, 5, 7, 12},
			expectedReplicas: 5,
			expectedReserveOrdinals: []intstr.IntOrString{
				intstr.FromInt32(1),
				intstr.FromInt32(2),
				intstr.FromInt32(4),
				intstr.FromInt32(6),
				intstr.FromInt32(8),
				intstr.FromInt32(9),
				intstr.FromInt32(10),
				intstr.FromInt32(11),
			}, // All ordinals from 0 to 12 that are NOT in activeNodes
		},
		{
			name:             "unsorted activeNodes are handled correctly",
			activeNodes:      []int32{5, 0, 3}, // Unsorted input
			expectedReplicas: 3,
			expectedReserveOrdinals: []intstr.IntOrString{
				intstr.FromInt32(1),
				intstr.FromInt32(2),
				intstr.FromInt32(4),
			},
		},
		{
			name:             "large gap at the beginning",
			activeNodes:      []int32{10, 11, 12},
			expectedReplicas: 3,
			expectedReserveOrdinals: []intstr.IntOrString{
				intstr.FromInt32(0),
				intstr.FromInt32(1),
				intstr.FromInt32(2),
				intstr.FromInt32(3),
				intstr.FromInt32(4),
				intstr.FromInt32(5),
				intstr.FromInt32(6),
				intstr.FromInt32(7),
				intstr.FromInt32(8),
				intstr.FromInt32(9),
			},
		},
		{
			name:             "alternating nodes",
			activeNodes:      []int32{0, 2, 4, 6},
			expectedReplicas: 4,
			expectedReserveOrdinals: []intstr.IntOrString{
				intstr.FromInt32(1),
				intstr.FromInt32(3),
				intstr.FromInt32(5),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replicas, reserveOrdinals := calculateReplicasAndReserveOrdinals(tt.activeNodes)

			assert.NotNil(t, replicas, "replicas should not be nil")
			assert.Equal(t, tt.expectedReplicas, *replicas, "replicas count mismatch")
			assert.Equal(t, tt.expectedReserveOrdinals, reserveOrdinals, "reserveOrdinals mismatch")

			// Verify that activeNodes + reserveOrdinals covers 0 to maxOrdinal
			if len(tt.activeNodes) > 0 && tt.expectedReserveOrdinals != nil {
				activeSet := make(map[int32]bool)
				for _, ord := range tt.activeNodes {
					activeSet[ord] = true
				}

				reservedSet := make(map[int32]bool)
				for _, ord := range reserveOrdinals {
					reservedSet[int32(ord.IntValue())] = true
				}

				// Find max ordinal
				maxOrd := int32(0)
				for _, ord := range tt.activeNodes {
					if ord > maxOrd {
						maxOrd = ord
					}
				}

				// Every ordinal from 0 to max should be either active or reserved
				for i := int32(0); i <= maxOrd; i++ {
					isActive := activeSet[i]
					isReserved := reservedSet[i]
					assert.True(t, isActive != isReserved,
						"ordinal %d should be exclusively in either activeNodes or reserveOrdinals", i)
				}
			}
		})
	}
}

func TestIsEphemeralNodesEnabled(t *testing.T) {
	tests := []struct {
		name           string
		ephemeralNodes *bool
		expected       bool
	}{
		{
			name:           "nil ephemeralNodes returns false",
			ephemeralNodes: nil,
			expected:       false,
		},
		{
			name:           "false ephemeralNodes returns false",
			ephemeralNodes: ptrBool(false),
			expected:       false,
		},
		{
			name:           "true ephemeralNodes returns true",
			ephemeralNodes: ptrBool(true),
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeSet := &values.SlurmNodeSet{
				EphemeralNodes: tt.ephemeralNodes,
			}
			result := isEphemeralNodesEnabled(nodeSet)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func ptrBool(b bool) *bool {
	return &b
}
