package values

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_mergeClusterExtra(t *testing.T) {
	t.Run("component-specific wins on conflicting keys", func(t *testing.T) {
		got := mergeClusterExtra(
			map[string]string{"a": "cluster", "b": "cluster"},
			map[string]string{"a": "component"},
		)
		assert.Equal(t, map[string]string{"a": "component", "b": "cluster"}, got)
	})

	t.Run("nil cluster-wide map returns component-specific as-is", func(t *testing.T) {
		componentSpecific := map[string]string{"a": "component"}
		got := mergeClusterExtra(nil, componentSpecific)
		assert.Equal(t, componentSpecific, got)
	})

	t.Run("nil component-specific map returns a clone of cluster-wide", func(t *testing.T) {
		got := mergeClusterExtra(map[string]string{"a": "cluster"}, nil)
		assert.Equal(t, map[string]string{"a": "cluster"}, got)
	})
}
