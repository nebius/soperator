package topologyconfcontroller_test

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
	tc "nebius.ai/slurm-operator/internal/controller/topologyconfcontroller"
)

func TestGetPodByNode(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}

	tests := []struct {
		name     string
		pods     []corev1.Pod
		expected map[string][]string
	}{
		{
			name: "Pods with NodeName",
			pods: []corev1.Pod{
				{Spec: corev1.PodSpec{NodeName: "node1"}, ObjectMeta: metav1.ObjectMeta{Name: "pod1"}},
				{Spec: corev1.PodSpec{NodeName: "node2"}, ObjectMeta: metav1.ObjectMeta{Name: "pod2"}},
				{Spec: corev1.PodSpec{NodeName: "node1"}, ObjectMeta: metav1.ObjectMeta{Name: "pod3"}},
			},
			expected: map[string][]string{
				"node1": {"pod1", "pod3"},
				"node2": {"pod2"},
			},
		},
		{
			name: "Pods without NodeName",
			pods: []corev1.Pod{
				{Spec: corev1.PodSpec{NodeName: ""}, ObjectMeta: metav1.ObjectMeta{Name: "pod1"}},
				{Spec: corev1.PodSpec{NodeName: ""}, ObjectMeta: metav1.ObjectMeta{Name: "pod2"}},
			},
			expected: map[string][]string{
				"root": {"pod1", "pod2"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.GetPodByNode(tt.pods)
			require.Equal(t, tt.expected, result, "Test %s failed: expected %v, got %v", tt.name, tt.expected, result)
		})
	}
}

func TestDeserializeNodeTopology(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}
	tests := []struct {
		name        string
		input       map[string]string
		expected    map[string]tc.NodeTopology
		expectError bool
	}{
		{
			name: "Valid topology data",
			input: map[string]string{
				"node1": `{"tier-1":"sw0","tier-2":"spine0"}`,
				"node2": `{"tier-1":"sw1","tier-2":"spine1","tier-3":"leaf0"}`,
			},
			expected: map[string]tc.NodeTopology{
				"node1": {"tier-1": "sw0", "tier-2": "spine0"},
				"node2": {"tier-1": "sw1", "tier-2": "spine1", "tier-3": "leaf0"},
			},
			expectError: false,
		},
		{
			name: "Invalid JSON data",
			input: map[string]string{
				"node1": `{"tier-1":"sw0","tier-2":"spine0"`, // Missing closing brace
			},
			expected:    nil,
			expectError: true,
		},
		{
			name:        "Empty input data",
			input:       map[string]string{},
			expected:    map[string]tc.NodeTopology{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := reconciler.DeserializeNodeTopology(tt.input)

			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				require.Nil(t, result, "Result should be nil when an error occurs")
			} else {
				require.NoError(t, err, "Unexpected error occurred")
				require.Equal(t, tt.expected, result, "Deserialized topology does not match expected result")
			}
		})
	}
}

func TestBuildTier1Links(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}
	tests := []struct {
		name          string
		tierNodes     map[string]tc.NodeTopology
		podsByNode    map[string][]string
		expectedLinks []string
	}{
		{
			name: "With root node",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
				"node2": {"tier-1": "switch2", "tier-2": "spine2"},
				"node3": {"tier-1": "switch1", "tier-2": "spine3"},
			},
			podsByNode: map[string][]string{
				"node1": {"pod1", "pod2"},
				"node2": {"pod3"},
				"node3": {"pod4"},
				"root":  {"pod5", "pod6"},
			},
			expectedLinks: []string{
				"SwitchName=switch1 Nodes=pod1,pod2,pod4",
				"SwitchName=switch2 Nodes=pod3",
				"SwitchName=root Nodes=pod5,pod6",
			},
		},
		{
			name: "Without root node",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
				"node2": {"tier-1": "switch2", "tier-2": "spine2"},
				"node3": {"tier-1": "switch1", "tier-2": "spine3"},
			},
			podsByNode: map[string][]string{
				"node1": {"pod1", "pod2"},
				"node2": {"pod3"},
				"node3": {"pod4"},
			},
			expectedLinks: []string{
				"SwitchName=switch1 Nodes=pod1,pod2,pod4",
				"SwitchName=switch2 Nodes=pod3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.BuildTier1Links(tt.tierNodes, tt.podsByNode)
			require.ElementsMatch(t, tt.expectedLinks, result)
		})
	}
}

func TestExtractTierNumber(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}
	tests := []struct {
		name     string
		tier     string
		expected int
	}{
		{
			name:     "Valid tier number",
			tier:     "tier-1",
			expected: 1,
		},
		{
			name:     "Valid tier number with double digits",
			tier:     "tier-10",
			expected: 10,
		},
		{
			name:     "Invalid tier format",
			tier:     "tier-abc",
			expected: 0,
		},
		{
			name:     "Empty string",
			tier:     "",
			expected: 0,
		},
		{
			name:     "No prefix",
			tier:     "1",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.ExtractTierNumber(tt.tier)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFindMaxTier(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}
	tests := []struct {
		name      string
		tierNodes map[string]tc.NodeTopology
		expected  int
	}{
		{
			name: "Multiple nodes with different tier levels",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1": "switch0",
					"tier-2": "leaf1",
					"tier-3": "spine1",
				},
				"node2": {
					"tier-1": "switch1",
					"tier-2": "leaf1",
					"tier-3": "spine1",
					"tier-4": "core1",
				},
				"node3": {
					"tier-1": "switch2",
					"tier-2": "leaf2",
				},
			},
			expected: 4,
		},
		{
			name: "Single tier topology",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1": "switch1",
				},
				"node2": {
					"tier-1": "switch2",
				},
			},
			expected: 1,
		},
		{
			name: "Two tier topology",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1": "switch1",
					"tier-2": "spine1",
				},
				"node2": {
					"tier-1": "switch2",
					"tier-2": "spine2",
				},
			},
			expected: 2,
		},
		{
			name: "High tier numbers",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1":  "switch1",
					"tier-5":  "level5",
					"tier-10": "level10",
				},
				"node2": {
					"tier-2": "switch2",
					"tier-7": "level7",
				},
			},
			expected: 10,
		},
		{
			name: "Non-sequential tier numbers",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1": "switch1",
					"tier-3": "level3",
					"tier-8": "level8",
				},
				"node2": {
					"tier-2": "switch2",
					"tier-6": "level6",
				},
			},
			expected: 8,
		},
		{
			name: "Mixed tier and non-tier keys",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1":   "switch1",
					"tier-3":   "spine1",
					"zone":     "us-west",
					"region":   "california",
					"non-tier": "value",
				},
				"node2": {
					"tier-2":   "leaf1",
					"hostname": "server1",
					"tier-5":   "core1",
				},
			},
			expected: 5,
		},
		{
			name:      "Empty topology",
			tierNodes: map[string]tc.NodeTopology{},
			expected:  0,
		},
		{
			name: "Nodes without tier keys",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"zone":     "us-west",
					"region":   "california",
					"hostname": "server1",
				},
				"node2": {
					"datacenter": "dc1",
					"rack":       "rack1",
				},
			},
			expected: 0,
		},
		{
			name: "Invalid tier keys (should be ignored)",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1":   "switch1",
					"tier-":    "invalid1",
					"tier-abc": "invalid2",
					"tier":     "invalid3",
					"tier-2":   "leaf1",
				},
			},
			expected: 2,
		},
		{
			name: "Zero tier number (edge case)",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-0": "switch0",
					"tier-1": "switch1",
					"tier-3": "spine1",
				},
			},
			expected: 3,
		},
		{
			name: "Single node with max tier",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-100": "super-core",
				},
			},
			expected: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.FindMaxTier(tt.tierNodes)
			if result != tt.expected {
				t.Errorf("FindMaxTier() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

func TestBuildHigherTierLinks(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}
	baseTopology := map[string]tc.NodeTopology{
		"node1": {
			"tier-1": "switch0",
			"tier-2": "leaf1",
			"tier-3": "spine1",
		},
		"node2": {
			"tier-1": "switch1",
			"tier-2": "leaf1",
			"tier-3": "spine1",
		},
		"node3": {
			"tier-1": "switch2",
			"tier-2": "leaf2",
			"tier-3": "spine1",
		},
		"node4": {
			"tier-1": "switch3",
			"tier-2": "leaf3",
			"tier-3": "spine3",
		},
	}

	tests := []struct {
		name      string
		tierNodes map[string]tc.NodeTopology
		expected  []string
	}{
		{
			name:      "Valid topology with multiple tiers - check all results",
			tierNodes: baseTopology,
			expected: []string{
				"SwitchName=leaf1 Switches=switch0,switch1",
				"SwitchName=leaf2 Switches=switch2",
				"SwitchName=leaf3 Switches=switch3",
				"SwitchName=spine1 Switches=leaf1,leaf2",
				"SwitchName=spine3 Switches=leaf3",
			},
		},

		{
			name: "Single tier topology",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1"},
				"node2": {"tier-1": "switch2"},
			},
			expected: []string{},
		},
		{
			name:      "Empty topology",
			tierNodes: map[string]tc.NodeTopology{},
			expected:  []string{},
		},

		{
			name: "Two tier topology",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
				"node2": {"tier-1": "switch2", "tier-2": "spine1"},
				"node3": {"tier-1": "switch3", "tier-2": "spine2"},
			},
			expected: []string{
				"SwitchName=spine1 Switches=switch1,switch2",
				"SwitchName=spine2 Switches=switch3",
			},
		},

		{
			name: "Four tier topology",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1": "switch1",
					"tier-2": "leaf1",
					"tier-3": "spine1",
					"tier-4": "core1",
				},
				"node2": {
					"tier-1": "switch2",
					"tier-2": "leaf1",
					"tier-3": "spine1",
					"tier-4": "core1",
				},
				"node3": {
					"tier-1": "switch3",
					"tier-2": "leaf2",
					"tier-3": "spine2",
					"tier-4": "core2",
				},
			},
			expected: []string{
				"SwitchName=leaf1 Switches=switch1,switch2",
				"SwitchName=leaf2 Switches=switch3",
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=spine2 Switches=leaf2",
				"SwitchName=core1 Switches=spine1",
				"SwitchName=core2 Switches=spine2",
			},
		},

		{
			name: "Incomplete tier topology",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1": "switch1",
					"tier-2": "leaf1",
					"tier-3": "spine1",
				},
				"node2": {
					"tier-1": "switch2",
					"tier-2": "leaf1", // tier-3 missing
				},
				"node3": {
					"tier-1": "switch3", // tier-2 and tier-3 missing
				},
				"node4": {
					"tier-2": "leaf2", // tier-1 missing
					"tier-3": "spine2",
				},
			},
			expected: []string{
				"SwitchName=leaf1 Switches=switch1,switch2",
				"SwitchName=spine1 Switches=leaf1",
				"SwitchName=spine2 Switches=leaf2",
			},
		},

		{
			name: "Non-sequential tier numbers",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1": "switch1",
					"tier-3": "spine1",
					"tier-7": "core1",
				},
				"node2": {
					"tier-1": "switch2",
					"tier-3": "spine1",
					"tier-7": "core1",
				},
				"node3": {
					"tier-2": "leaf1",
					"tier-3": "spine2",
				},
			},
			expected: []string{
				"SwitchName=spine2 Switches=leaf1",
			},
		},

		{
			name: "Duplicate devices in same tier",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1"}, // Duplicate
				"node2": {"tier-1": "switch1", "tier-2": "leaf1"}, // Duplicate
				"node3": {"tier-1": "switch2", "tier-2": "leaf1"},
			},
			expected: []string{
				"SwitchName=leaf1 Switches=switch1,switch2",
			},
		},

		{
			name: "Complex topology with many connections",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1"},
				"node2": {"tier-1": "switch2", "tier-2": "leaf2"},
				"node3": {"tier-1": "switch3", "tier-2": "leaf3"},
				"node4": {"tier-1": "switch4", "tier-2": "leaf1"},
				"node5": {"tier-1": "switch5", "tier-2": "leaf2"},
			},
			expected: []string{
				"SwitchName=leaf1 Switches=switch1,switch4",
				"SwitchName=leaf2 Switches=switch2,switch5",
				"SwitchName=leaf3 Switches=switch3",
			},
		},

		{
			name: "High tier numbers",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-5":  "device5-1",
					"tier-10": "device10-1",
					"tier-15": "device15-1",
				},
				"node2": {
					"tier-5":  "device5-2",
					"tier-10": "device10-1",
					"tier-15": "device15-1",
				},
			},
			expected: []string{},
		},

		{
			name: "Mixed tier and non-tier keys",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1":   "switch1",
					"tier-2":   "leaf1",
					"zone":     "us-west",
					"region":   "california",
					"hostname": "server1",
					"non-tier": "value1",
				},
				"node2": {
					"tier-1":     "switch2",
					"tier-2":     "leaf1",
					"datacenter": "dc1",
					"rack":       "rack1",
				},
			},
			expected: []string{
				"SwitchName=leaf1 Switches=switch1,switch2",
			},
		},

		{
			name: "Empty tier values",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {
					"tier-1": "",
					"tier-2": "leaf1",
				},
				"node2": {
					"tier-1": "switch1",
					"tier-2": "",
				},
			},
			expected: []string{},
		},

		{
			name: "Single node per tier level",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1", "tier-3": "spine1"},
			},
			expected: []string{
				"SwitchName=leaf1 Switches=switch1",
				"SwitchName=spine1 Switches=leaf1",
			},
		},

		{
			name: "Check result sorting",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "z-switch", "tier-2": "z-leaf"},
				"node2": {"tier-1": "a-switch", "tier-2": "a-leaf"},
				"node3": {"tier-1": "m-switch", "tier-2": "m-leaf"},
			},
			expected: []string{
				"SwitchName=a-leaf Switches=a-switch",
				"SwitchName=m-leaf Switches=m-switch",
				"SwitchName=z-leaf Switches=z-switch",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.BuildHigherTierLinks(tt.tierNodes)

			if len(result) != len(tt.expected) {
				t.Errorf("Test %s failed: expected %d results, got %d. Expected: %v, Got: %v",
					tt.name, len(tt.expected), len(result), tt.expected, result)
				return
			}

			if len(tt.expected) == 0 {
				require.Empty(t, result, "Expected empty result for test %s", tt.name)
				return
			}

			for _, expectedStr := range tt.expected {
				found := false
				for _, resultStr := range result {
					if resultStr == expectedStr {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Test %s failed: expected string '%s' not found in result %v",
						tt.name, expectedStr, result)
				}
			}

			for _, resultStr := range result {
				found := false
				for _, expectedStr := range tt.expected {
					if resultStr == expectedStr {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Test %s failed: unexpected string '%s' found in result %v",
						tt.name, resultStr, result)
				}
			}
		})
	}
}

func TestBuildLinksForTierWithSlices(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}
	baseTopology := map[string]tc.NodeTopology{
		"node1": {
			"tier-1": "switch0",
			"tier-2": "leaf1",
			"tier-3": "spine1",
			"tier-4": "core1",
		},
		"node2": {
			"tier-1": "switch1",
			"tier-2": "leaf1",
			"tier-3": "spine1",
			"tier-4": "core1",
		},
		"node3": {
			"tier-1": "switch2",
			"tier-2": "leaf2",
			"tier-3": "spine1",
			"tier-4": "core2",
		},
		"node4": {
			"tier-1": "switch3",
			"tier-2": "leaf3",
			"tier-3": "spine2",
			"tier-4": "core2",
		},
	}

	tests := []struct {
		name        string
		tierNodes   map[string]tc.NodeTopology
		currentTier int
		expected    []string
	}{
		{
			name:        "Tier-2 links from base topology",
			tierNodes:   baseTopology,
			currentTier: 2,
			expected: []string{
				"SwitchName=leaf1 Switches=switch0,switch1",
				"SwitchName=leaf2 Switches=switch2",
				"SwitchName=leaf3 Switches=switch3",
			},
		},

		{
			name:        "Tier-3 links from base topology",
			tierNodes:   baseTopology,
			currentTier: 3,
			expected: []string{
				"SwitchName=spine1 Switches=leaf1,leaf2",
				"SwitchName=spine2 Switches=leaf3",
			},
		},

		{
			name:        "Tier-4 links from base topology",
			tierNodes:   baseTopology,
			currentTier: 4,
			expected: []string{
				"SwitchName=core1 Switches=spine1",
				"SwitchName=core2 Switches=spine1,spine2",
			},
		},

		{
			name: "Simple two-tier topology",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1", "tier-2": "spine1"},
				"node2": {"tier-1": "switch2", "tier-2": "spine1"},
				"node3": {"tier-1": "switch3", "tier-2": "spine2"},
			},
			currentTier: 2,
			expected: []string{
				"SwitchName=spine1 Switches=switch1,switch2",
				"SwitchName=spine2 Switches=switch3",
			},
		},

		{
			name: "Missing lower tier",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-2": "leaf1", "tier-3": "spine1"},
				"node2": {"tier-2": "leaf2", "tier-3": "spine1"},
			},
			currentTier: 2, // tier-1 missing
			expected:    []string{},
		},

		{
			name: "Missing upper tier",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1"},
				"node2": {"tier-1": "switch2"},
			},
			currentTier: 2, // tier-2 missing
			expected:    []string{},
		},

		{
			name:        "Empty topology",
			tierNodes:   map[string]tc.NodeTopology{},
			currentTier: 2,
			expected:    []string{},
		},

		{
			name: "Single node topology",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1"},
			},
			currentTier: 2,
			expected: []string{
				"SwitchName=leaf1 Switches=switch1",
			},
		},
		{
			name: "Duplicate devices",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1"}, // Dublicate
				"node2": {"tier-1": "switch1", "tier-2": "leaf1"}, // Dublicate
				"node3": {"tier-1": "switch2", "tier-2": "leaf1"},
			},
			currentTier: 2,
			expected: []string{
				"SwitchName=leaf1 Switches=switch1,switch2",
			},
		},

		{
			name: "Multiple same lower tier devices",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1"},
				"node2": {"tier-1": "switch1", "tier-2": "leaf1"},
				"node3": {"tier-1": "switch1", "tier-2": "leaf2"},
			},
			currentTier: 2,
			expected: []string{
				"SwitchName=leaf1 Switches=switch1",
				"SwitchName=leaf2 Switches=switch1",
			},
		},

		{
			name: "High tier numbers",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-9": "device9-1", "tier-10": "device10-1"},
				"node2": {"tier-9": "device9-2", "tier-10": "device10-1"},
				"node3": {"tier-9": "device9-3", "tier-10": "device10-2"},
			},
			currentTier: 10,
			expected: []string{
				"SwitchName=device10-1 Switches=device9-1,device9-2",
				"SwitchName=device10-2 Switches=device9-3",
			},
		},

		{
			name: "Incomplete tier data",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "switch1", "tier-2": "leaf1", "tier-3": "spine1"},
				"node2": {"tier-1": "switch2", "tier-2": "leaf2"}, // tier-3 missing
				"node3": {"tier-2": "leaf3", "tier-3": "spine2"},  // tier-1 missing
				"node4": {"tier-1": "switch4"},
			},
			currentTier: 2,
			expected: []string{
				"SwitchName=leaf1 Switches=switch1",
				"SwitchName=leaf2 Switches=switch2",
			},
		},

		{
			name: "Empty tier values",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "", "tier-2": "leaf1"},
				"node2": {"tier-1": "switch1", "tier-2": ""},
				"node3": {"tier-1": "switch2", "tier-2": "leaf2"},
			},
			currentTier: 2,
			expected: []string{
				"SwitchName=leaf2 Switches=switch2",
			},
		},

		{
			name: "Device sorting test",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-1": "z-switch", "tier-2": "a-leaf"},
				"node2": {"tier-1": "a-switch", "tier-2": "a-leaf"},
				"node3": {"tier-1": "m-switch", "tier-2": "a-leaf"},
				"node4": {"tier-1": "b-switch", "tier-2": "z-leaf"},
			},
			currentTier: 2,
			expected: []string{
				"SwitchName=a-leaf Switches=a-switch,m-switch,z-switch",
				"SwitchName=z-leaf Switches=b-switch",
			},
		},

		{
			name: "Tier-1 with tier-0",
			tierNodes: map[string]tc.NodeTopology{
				"node1": {"tier-0": "port1", "tier-1": "switch1"},
				"node2": {"tier-0": "port2", "tier-1": "switch1"},
				"node3": {"tier-0": "port3", "tier-1": "switch2"},
			},
			currentTier: 1,
			expected: []string{
				"SwitchName=switch1 Switches=port1,port2",
				"SwitchName=switch2 Switches=port3",
			},
		},

		{
			name: "Many devices test",
			tierNodes: func() map[string]tc.NodeTopology {
				nodes := make(map[string]tc.NodeTopology)
				for i := 0; i < 10; i++ {
					nodeName := fmt.Sprintf("node%d", i)
					nodes[nodeName] = tc.NodeTopology{
						"tier-1": fmt.Sprintf("switch%d", i),
						"tier-2": fmt.Sprintf("leaf%d", i/3), // Группируем по 3
					}
				}
				return nodes
			}(),
			currentTier: 2,
			expected: []string{
				"SwitchName=leaf0 Switches=switch0,switch1,switch2",
				"SwitchName=leaf1 Switches=switch3,switch4,switch5",
				"SwitchName=leaf2 Switches=switch6,switch7,switch8",
				"SwitchName=leaf3 Switches=switch9",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.BuildLinksForTierWithSlices(tt.tierNodes, tt.currentTier)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d results, got %d. Expected: %v, Got: %v",
					len(tt.expected), len(result), tt.expected, result)
				return
			}

			for i, expected := range tt.expected {
				if i >= len(result) {
					t.Errorf("Missing result at index %d. Expected: %s", i, expected)
					continue
				}
				if result[i] != expected {
					t.Errorf("Result at index %d doesn't match. Expected: %s, Got: %s",
						i, expected, result[i])
				}
			}
		})
	}
}

func TestBuildLinksForTierWithSlicesEdgeCases(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}
	tests := []struct {
		name        string
		tierNodes   map[string]tc.NodeTopology
		currentTier int
		expected    []string
	}{
		{
			name:        "Tier 0 (no lower tier)",
			tierNodes:   map[string]tc.NodeTopology{"node1": {"tier-0": "device0"}},
			currentTier: 0,
			expected:    []string{},
		},
		{
			name:        "Negative tier",
			tierNodes:   map[string]tc.NodeTopology{"node1": {"tier-1": "device1"}},
			currentTier: -1,
			expected:    []string{},
		},
		{
			name:        "Very high tier number",
			tierNodes:   map[string]tc.NodeTopology{"node1": {"tier-99": "device99", "tier-100": "device100"}},
			currentTier: 100,
			expected:    []string{"SwitchName=device100 Switches=device99"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.BuildLinksForTierWithSlices(tt.tierNodes, tt.currentTier)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d results, got %d. Expected: %v, Got: %v",
					len(tt.expected), len(result), tt.expected, result)
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("Result at index %d doesn't match. Expected: %s, Got: %s",
						i, expected, result[i])
				}
			}
		})
	}
}

func TestBuildLinksForTierWithSlicesSorting(t *testing.T) {
	reconciler := &tc.WorkerTopologyReconciler{}
	tierNodes := map[string]tc.NodeTopology{
		"node1": {"tier-1": "z-device", "tier-2": "z-upper"},
		"node2": {"tier-1": "a-device", "tier-2": "a-upper"},
		"node3": {"tier-1": "m-device", "tier-2": "m-upper"},
		"node4": {"tier-1": "b-device", "tier-2": "a-upper"},
	}

	result := reconciler.BuildLinksForTierWithSlices(tierNodes, 2)

	expected := []string{
		"SwitchName=a-upper Switches=a-device,b-device",
		"SwitchName=m-upper Switches=m-device",
		"SwitchName=z-upper Switches=z-device",
	}

	if len(result) != len(expected) {
		t.Errorf("Expected %d results, got %d", len(expected), len(result))
		return
	}

	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("Result not sorted correctly at index %d. Expected: %s, Got: %s",
				i, exp, result[i])
		}
	}
}
