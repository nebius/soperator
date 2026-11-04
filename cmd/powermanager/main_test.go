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

package main

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func TestNodeSetIsEphemeral(t *testing.T) {
	for _, tc := range []struct {
		name           string
		ephemeral      *bool
		missingNodeSet bool
		want           bool
	}{
		{name: "ephemeral", ephemeral: ptr.To(true), want: true},
		{name: "non-ephemeral", ephemeral: ptr.To(false)},
		{name: "unset defaults to non-ephemeral"},
		{name: "missing nodeset", missingNodeSet: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if !tc.missingNodeSet {
				nodeSet := &slurmv1alpha1.NodeSet{
					ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test"},
				}
				nodeSet.Spec.EphemeralNodes = tc.ephemeral
				builder.WithObjects(nodeSet)
			}

			got, err := nodeSetIsEphemeral(context.Background(), builder.Build(), "test", "worker")
			if err != nil {
				t.Fatalf("nodeSetIsEphemeral returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("nodeSetIsEphemeral = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNodeSetIsEphemeralRetriesTransientErrors(t *testing.T) {
	for _, transient := range []error{
		apierrors.NewServiceUnavailable("try again"),
		apierrors.NewInternalError(fmt.Errorf("try again")),
		apierrors.NewTooManyRequests("try again", 0),
		&net.DNSError{IsTimeout: true},
	} {
		t.Run(transient.Error(), func(t *testing.T) {
			nodeSet := &slurmv1alpha1.NodeSet{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test"}}
			nodeSet.Spec.EphemeralNodes = ptr.To(true)
			calls := 0
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(nodeSet).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c ctrlclient.WithWatch, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
					calls++
					if calls == 1 {
						return fmt.Errorf("read object: %w", transient)
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).Build()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			ephemeral, err := nodeSetIsEphemeral(ctx, client, "test", "worker")
			require.NoError(t, err)
			require.True(t, ephemeral)
			require.Equal(t, 2, calls)
		})
	}
}

func TestNodeSetIsEphemeralStopsRetrying(t *testing.T) {
	for _, tt := range []struct {
		name      string
		err, want error
	}{
		{name: "deadline", err: apierrors.NewServiceUnavailable("try again"), want: context.DeadlineExceeded},
		{name: "forbidden", err: apierrors.NewForbidden(schema.GroupResource{Resource: "nodesets"}, "worker", fmt.Errorf("denied"))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(context.Context, ctrlclient.WithWatch, ctrlclient.ObjectKey, ctrlclient.Object, ...ctrlclient.GetOption) error {
					calls++
					return tt.err
				},
			}).Build()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			ephemeral, err := nodeSetIsEphemeral(ctx, client, "test", "worker")
			require.False(t, ephemeral)
			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
			} else {
				require.ErrorIs(t, err, tt.err)
			}
			require.Equal(t, 1, calls)
		})
	}
}

func TestParseNodeList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []NodeRef
		wantErr  bool
	}{
		{
			name:  "single node",
			input: "worker-0",
			expected: []NodeRef{
				{NodeSetName: "worker", Ordinal: 0},
			},
		},
		{
			name:  "simple range",
			input: "worker-[0-2]",
			expected: []NodeRef{
				{NodeSetName: "worker", Ordinal: 0},
				{NodeSetName: "worker", Ordinal: 1},
				{NodeSetName: "worker", Ordinal: 2},
			},
		},
		{
			name:  "complex range with gaps",
			input: "worker-[0-2,5,7-8]",
			expected: []NodeRef{
				{NodeSetName: "worker", Ordinal: 0},
				{NodeSetName: "worker", Ordinal: 1},
				{NodeSetName: "worker", Ordinal: 2},
				{NodeSetName: "worker", Ordinal: 5},
				{NodeSetName: "worker", Ordinal: 7},
				{NodeSetName: "worker", Ordinal: 8},
			},
		},
		{
			name:  "multiple node sets",
			input: "worker-[0-2],gpu-[1-2]",
			expected: []NodeRef{
				{NodeSetName: "worker", Ordinal: 0},
				{NodeSetName: "worker", Ordinal: 1},
				{NodeSetName: "worker", Ordinal: 2},
				{NodeSetName: "gpu", Ordinal: 1},
				{NodeSetName: "gpu", Ordinal: 2},
			},
		},
		{
			name:  "mixed single and range",
			input: "worker-0,gpu-[1-3]",
			expected: []NodeRef{
				{NodeSetName: "worker", Ordinal: 0},
				{NodeSetName: "gpu", Ordinal: 1},
				{NodeSetName: "gpu", Ordinal: 2},
				{NodeSetName: "gpu", Ordinal: 3},
			},
		},
		{
			name:  "node set with hyphen in name",
			input: "compute-worker-[0-1]",
			expected: []NodeRef{
				{NodeSetName: "compute-worker", Ordinal: 0},
				{NodeSetName: "compute-worker", Ordinal: 1},
			},
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseNodeList(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseNodeList(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSplitNodeList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single node",
			input:    "worker-0",
			expected: []string{"worker-0"},
		},
		{
			name:     "multiple nodes separated by comma",
			input:    "worker-0,worker-1",
			expected: []string{"worker-0", "worker-1"},
		},
		{
			name:     "range with comma inside brackets",
			input:    "worker-[0,2,5]",
			expected: []string{"worker-[0,2,5]"},
		},
		{
			name:     "complex with multiple ranges",
			input:    "worker-[0-2,5],gpu-[1,3-5]",
			expected: []string{"worker-[0-2,5]", "gpu-[1,3-5]"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "spaces around commas",
			input:    "worker-0 , worker-1",
			expected: []string{"worker-0", "worker-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitNodeList(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("splitNodeList(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseRangeSpec(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []int32
		wantErr  bool
	}{
		{
			name:     "single number",
			input:    "5",
			expected: []int32{5},
		},
		{
			name:     "simple range",
			input:    "0-3",
			expected: []int32{0, 1, 2, 3},
		},
		{
			name:     "multiple numbers",
			input:    "1,3,5",
			expected: []int32{1, 3, 5},
		},
		{
			name:     "mixed ranges and numbers",
			input:    "0-2,5,7-8",
			expected: []int32{0, 1, 2, 5, 7, 8},
		},
		{
			name:    "invalid number",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "invalid range",
			input:   "1-abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseRangeSpec(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseRangeSpec(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGroupNodesByNodeSet(t *testing.T) {
	tests := []struct {
		name     string
		input    []NodeRef
		expected map[string][]int32
	}{
		{
			name:     "empty input",
			input:    []NodeRef{},
			expected: map[string][]int32{},
		},
		{
			name: "single node set",
			input: []NodeRef{
				{NodeSetName: "worker", Ordinal: 0},
				{NodeSetName: "worker", Ordinal: 1},
				{NodeSetName: "worker", Ordinal: 2},
			},
			expected: map[string][]int32{
				"worker": {0, 1, 2},
			},
		},
		{
			name: "multiple node sets",
			input: []NodeRef{
				{NodeSetName: "worker", Ordinal: 0},
				{NodeSetName: "gpu", Ordinal: 1},
				{NodeSetName: "worker", Ordinal: 2},
				{NodeSetName: "gpu", Ordinal: 3},
			},
			expected: map[string][]int32{
				"worker": {0, 2},
				"gpu":    {1, 3},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := groupNodesByNodeSet(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("groupNodesByNodeSet() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseNodeRange(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []NodeRef
		wantErr  bool
	}{
		{
			name:  "simple single node",
			input: "worker-5",
			expected: []NodeRef{
				{NodeSetName: "worker", Ordinal: 5},
			},
		},
		{
			name:  "range with brackets",
			input: "worker-[0-2]",
			expected: []NodeRef{
				{NodeSetName: "worker", Ordinal: 0},
				{NodeSetName: "worker", Ordinal: 1},
				{NodeSetName: "worker", Ordinal: 2},
			},
		},
		{
			name:  "hyphenated node set name",
			input: "my-compute-worker-[0-1]",
			expected: []NodeRef{
				{NodeSetName: "my-compute-worker", Ordinal: 0},
				{NodeSetName: "my-compute-worker", Ordinal: 1},
			},
		},
		{
			name:    "no ordinal",
			input:   "worker",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseNodeRange(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseNodeRange(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCheckNodesStatusLogic(t *testing.T) {
	// Testing the logic of checking nodes without Kubernetes client
	// This simulates what checkNodesStatus does

	tests := []struct {
		name        string
		activeNodes []int32
		ordinals    []int32
		checkAdded  bool
		expected    bool
	}{
		{
			name:        "all nodes present - check added",
			activeNodes: []int32{0, 1, 2, 3},
			ordinals:    []int32{1, 2},
			checkAdded:  true,
			expected:    true,
		},
		{
			name:        "some nodes missing - check added",
			activeNodes: []int32{0, 1},
			ordinals:    []int32{1, 2},
			checkAdded:  true,
			expected:    false,
		},
		{
			name:        "all nodes removed - check removed",
			activeNodes: []int32{0, 3},
			ordinals:    []int32{1, 2},
			checkAdded:  false,
			expected:    true,
		},
		{
			name:        "some nodes still present - check removed",
			activeNodes: []int32{0, 1, 3},
			ordinals:    []int32{1, 2},
			checkAdded:  false,
			expected:    false,
		},
		{
			name:        "empty active nodes - check added",
			activeNodes: []int32{},
			ordinals:    []int32{1, 2},
			checkAdded:  true,
			expected:    false,
		},
		{
			name:        "empty active nodes - check removed",
			activeNodes: []int32{},
			ordinals:    []int32{1, 2},
			checkAdded:  false,
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the logic from checkNodesStatus
			activeSet := make(map[int32]bool)
			for _, ordinal := range tt.activeNodes {
				activeSet[ordinal] = true
			}

			var result bool
			if tt.checkAdded {
				result = true
				for _, ordinal := range tt.ordinals {
					if !activeSet[ordinal] {
						result = false
						break
					}
				}
			} else {
				result = true
				for _, ordinal := range tt.ordinals {
					if activeSet[ordinal] {
						result = false
						break
					}
				}
			}

			if result != tt.expected {
				t.Errorf("checkNodesStatus logic: activeNodes=%v, ordinals=%v, checkAdded=%v = %v, want %v",
					tt.activeNodes, tt.ordinals, tt.checkAdded, result, tt.expected)
			}
		})
	}
}

func TestUpdateActiveNodesLogic(t *testing.T) {
	// Test the logic of updating activeNodes without Kubernetes client

	tests := []struct {
		name           string
		currentActive  []int32
		ordinals       []int32
		resume         bool
		expectedActive []int32
	}{
		{
			name:           "resume - add new nodes",
			currentActive:  []int32{0, 1},
			ordinals:       []int32{2, 3},
			resume:         true,
			expectedActive: []int32{0, 1, 2, 3},
		},
		{
			name:           "resume - add existing nodes (idempotent)",
			currentActive:  []int32{0, 1, 2},
			ordinals:       []int32{1, 2},
			resume:         true,
			expectedActive: []int32{0, 1, 2},
		},
		{
			name:           "suspend - remove nodes",
			currentActive:  []int32{0, 1, 2, 3},
			ordinals:       []int32{1, 2},
			resume:         false,
			expectedActive: []int32{0, 3},
		},
		{
			name:           "suspend - remove non-existing nodes (idempotent)",
			currentActive:  []int32{0, 3},
			ordinals:       []int32{1, 2},
			resume:         false,
			expectedActive: []int32{0, 3},
		},
		{
			name:           "resume - start from empty",
			currentActive:  []int32{},
			ordinals:       []int32{0, 1, 2},
			resume:         true,
			expectedActive: []int32{0, 1, 2},
		},
		{
			name:           "suspend - remove all",
			currentActive:  []int32{0, 1, 2},
			ordinals:       []int32{0, 1, 2},
			resume:         false,
			expectedActive: []int32{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the logic from updateNodeSetPowerState
			currentActiveSet := make(map[int32]bool)
			for _, ord := range tt.currentActive {
				currentActiveSet[ord] = true
			}

			if tt.resume {
				for _, ord := range tt.ordinals {
					currentActiveSet[ord] = true
				}
			} else {
				for _, ord := range tt.ordinals {
					delete(currentActiveSet, ord)
				}
			}

			newActiveNodes := make([]int32, 0, len(currentActiveSet))
			for ord := range currentActiveSet {
				newActiveNodes = append(newActiveNodes, ord)
			}
			sort.Slice(newActiveNodes, func(i, j int) bool { return newActiveNodes[i] < newActiveNodes[j] })

			if !reflect.DeepEqual(newActiveNodes, tt.expectedActive) {
				t.Errorf("updateActiveNodes logic: current=%v, ordinals=%v, resume=%v = %v, want %v",
					tt.currentActive, tt.ordinals, tt.resume, newActiveNodes, tt.expectedActive)
			}
		})
	}
}
