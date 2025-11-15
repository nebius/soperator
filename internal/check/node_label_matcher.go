package check

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// NodeLabelMatcher handles matching of node labels against configured ignored labels.
type NodeLabelMatcher struct {
	ignoredLabels map[string]string
}

// NewNodeLabelMatcher creates a new NodeLabelMatcher from a comma-separated string of label pairs.
// The format is "key1=value1,key2=value2".
// Returns an error if the format is invalid.
func NewNodeLabelMatcher(maintenanceIgnoreNodeLabels string) (*NodeLabelMatcher, error) {
	matcher := &NodeLabelMatcher{
		ignoredLabels: make(map[string]string),
	}

	if maintenanceIgnoreNodeLabels == "" {
		return matcher, nil
	}

	pairs := strings.Split(maintenanceIgnoreNodeLabels, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid label format: %q, expected key=value", pair)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if key == "" || value == "" {
			return nil, fmt.Errorf("invalid label format: %q, key and value cannot be empty", pair)
		}

		matcher.ignoredLabels[key] = value
	}

	return matcher, nil
}

// ShouldIgnoreNode returns true if the node has any of the ignored labels.
func (m *NodeLabelMatcher) ShouldIgnoreNode(node *corev1.Node) bool {
	if node == nil || len(m.ignoredLabels) == 0 {
		return false
	}

	for key, expectedValue := range m.ignoredLabels {
		if actualValue, exists := node.Labels[key]; exists && actualValue == expectedValue {
			return true
		}
	}

	return false
}

// GetIgnoredLabels returns a copy of the ignored labels map.
func (m *NodeLabelMatcher) GetIgnoredLabels() map[string]string {
	if m.ignoredLabels == nil {
		return make(map[string]string)
	}

	labels := make(map[string]string, len(m.ignoredLabels))
	for k, v := range m.ignoredLabels {
		labels[k] = v
	}
	return labels
}

// HasIgnoredLabels returns true if there are any ignored labels configured.
func (m *NodeLabelMatcher) HasIgnoredLabels() bool {
	return len(m.ignoredLabels) > 0
}
