package sliceutils_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils/sliceutils"
)

func TestFilterDeprecated(t *testing.T) {
	t.Run("Test Filter empty", func(t *testing.T) {
		f := sliceutils.Filter(emptyTestCases, func(t TestCase) bool {
			return t.A == 10
		})
		assert.Empty(t, f)
	})

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

func TestFilter(t *testing.T) {
	for _, suite := range []struct {
		Name string
		Impl func([]TestCase, func(testCase TestCase) bool) []TestCase
	}{
		{
			Name: "FilterSlice",
			Impl: func(xs []TestCase, pred func(TestCase) bool) []TestCase {
				return sliceutils.FilterSlice(xs, pred)
			},
		},
		{
			Name: "FilterSliceSeq",
			Impl: func(xs []TestCase, pred func(TestCase) bool) []TestCase {
				return sliceutils.Collect(sliceutils.FilterSliceSeq(xs, pred))
			},
		},
		{
			Name: "FilterSeqSlice",
			Impl: func(xs []TestCase, pred func(TestCase) bool) []TestCase {
				return sliceutils.FilterSeqSlice(sliceutils.SliceSeq(xs), pred)
			},
		},
		{
			Name: "FilterSeq",
			Impl: func(xs []TestCase, pred func(TestCase) bool) []TestCase {
				return sliceutils.Collect(sliceutils.FilterSeq(sliceutils.SliceSeq(xs), pred))
			},
		},
	} {
		t.Run(fmt.Sprintf("Test %s empty", suite.Name), func(t *testing.T) {
			f := suite.Impl(emptyTestCases, func(t TestCase) bool {
				return t.A == 10
			})
			assert.Empty(t, f)
		})

		t.Run(fmt.Sprintf("Test %s found", suite.Name), func(t *testing.T) {
			f := suite.Impl(testCases, func(t TestCase) bool {
				return t.A == 10
			})
			assert.Equal(t, "hello", f[0].B)

			f = suite.Impl(testCases, func(t TestCase) bool {
				return t.B == "bye"
			})
			assert.Equal(t, 20, f[0].A)
		})

		t.Run(fmt.Sprintf("Test %s not found", suite.Name), func(t *testing.T) {
			f := suite.Impl(testCases, func(t TestCase) bool {
				return t.A == 0
			})
			assert.Empty(t, f)
		})
	}
}
