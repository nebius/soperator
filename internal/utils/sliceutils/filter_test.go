package sliceutils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils/sliceutils"
)

func TestFilter(t *testing.T) {
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

func TestFilterSlice(t *testing.T) {
	t.Run("Test FilterSlice empty", func(t *testing.T) {
		f := sliceutils.FilterSlice(emptyTestCases, func(t TestCase) bool {
			return t.A == 10
		})
		assert.Empty(t, f)
	})

	t.Run("Test FilterSlice found", func(t *testing.T) {
		f := sliceutils.FilterSlice(testCases, func(t TestCase) bool {
			return t.A == 10
		})
		assert.Equal(t, "hello", f[0].B)

		f = sliceutils.FilterSlice(testCases, func(t TestCase) bool {
			return t.B == "bye"
		})
		assert.Equal(t, 20, f[0].A)
	})

	t.Run("Test FilterSlice not found", func(t *testing.T) {
		f := sliceutils.FilterSlice(testCases, func(t TestCase) bool {
			return t.A == 0
		})
		assert.Empty(t, f)
	})
}

func TestFilterSliceSeq(t *testing.T) {
	t.Run("Test FilterSliceSeq empty", func(t *testing.T) {
		f := sliceutils.FilterSliceSeq(emptyTestCases, func(t TestCase) bool {
			return t.A == 10
		})
		assert.True(t, sliceutils.IsEmptySeq(f))
	})

	t.Run("Test FilterSliceSeq found", func(t *testing.T) {
		f := sliceutils.FilterSliceSeq(testCases, func(t TestCase) bool {
			return t.A == 10
		})
		assert.Equal(t, "hello", sliceutils.Collect(f)[0].B)

		f = sliceutils.FilterSliceSeq(testCases, func(t TestCase) bool {
			return t.B == "bye"
		})
		assert.Equal(t, 20, sliceutils.Collect(f)[0].A)
	})

	t.Run("Test FilterSliceSeq not found", func(t *testing.T) {
		f := sliceutils.FilterSliceSeq(testCases, func(t TestCase) bool {
			return t.A == 0
		})
		assert.Empty(t, sliceutils.Collect(f))
	})
}

func TestFilterSeqSlice(t *testing.T) {
	t.Run("Test FilterSeqSlice empty", func(t *testing.T) {
		f := sliceutils.FilterSeqSlice(sliceutils.SliceSeq(emptyTestCases), func(t TestCase) bool {
			return t.A == 10
		})
		assert.Empty(t, f)
	})

	t.Run("Test FilterSeqSlice found", func(t *testing.T) {
		f := sliceutils.FilterSeqSlice(sliceutils.SliceSeq(testCases), func(t TestCase) bool {
			return t.A == 10
		})
		assert.Equal(t, "hello", f[0].B)

		f = sliceutils.FilterSeqSlice(sliceutils.SliceSeq(testCases), func(t TestCase) bool {
			return t.B == "bye"
		})
		assert.Equal(t, 20, f[0].A)
	})

	t.Run("Test FilterSeqSlice not found", func(t *testing.T) {
		f := sliceutils.FilterSeqSlice(sliceutils.SliceSeq(testCases), func(t TestCase) bool {
			return t.A == 0
		})
		assert.Empty(t, f)
	})
}

func TestFilterSeq(t *testing.T) {
	t.Run("Test FilterSeq empty", func(t *testing.T) {
		f := sliceutils.FilterSeq(sliceutils.SliceSeq(emptyTestCases), func(t TestCase) bool {
			return t.A == 10
		})
		assert.True(t, sliceutils.IsEmptySeq(f))
	})

	t.Run("Test FilterSeq found", func(t *testing.T) {
		f := sliceutils.FilterSeq(sliceutils.SliceSeq(testCases), func(t TestCase) bool {
			return t.A == 10
		})
		assert.Equal(t, "hello", sliceutils.Collect(f)[0].B)

		f = sliceutils.FilterSeq(sliceutils.SliceSeq(testCases), func(t TestCase) bool {
			return t.B == "bye"
		})
		assert.Equal(t, 20, sliceutils.Collect(f)[0].A)
	})

	t.Run("Test FilterSeq not found", func(t *testing.T) {
		f := sliceutils.FilterSeq(sliceutils.SliceSeq(testCases), func(t TestCase) bool {
			return t.A == 0
		})
		assert.Empty(t, sliceutils.Collect(f))
	})
}
