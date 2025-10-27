package sliceutils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils/sliceutils"
)

var (
	filterTestCases = []TestCase{{
		A: 10,
		B: "hello",
	}, {
		A: 20,
		B: "bye",
	}}
)

func TestFilter(t *testing.T) {
	t.Run("Test Filter found", func(t *testing.T) {
		f := sliceutils.Filter(filterTestCases, func(t TestCase) bool {
			return t.A == 10
		})
		assert.Equal(t, "hello", f[0].B)

		f = sliceutils.Filter(filterTestCases, func(t TestCase) bool {
			return t.B == "bye"
		})
		assert.Equal(t, 20, f[0].A)
	})

	t.Run("Test Filter not found", func(t *testing.T) {
		f := sliceutils.Filter(filterTestCases, func(t TestCase) bool {
			return t.A == 0
		})
		assert.Empty(t, f)
	})
}
