package sliceutils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils/sliceutils"
)

func TestFilter(t *testing.T) {
	t.Run("Test Filter found", func(t *testing.T) {
		f := sliceutils.Filter(testCases, func(t TestCase) bool {
			return t.A == 10
		})
		assert.Equal(t, "hello", f[0].B)

		f = sliceutils.Filter(testCases, func(t TestCase) bool {
			return t.B == "bye"
		})
		assert.Equal(t, 20, f[0].A)
	})

	t.Run("Test Filter not found", func(t *testing.T) {
		f := sliceutils.Filter(testCases, func(t TestCase) bool {
			return t.A == 0
		})
		assert.Empty(t, f)
	})
}
