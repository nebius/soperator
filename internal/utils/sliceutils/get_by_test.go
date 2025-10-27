package sliceutils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils/sliceutils"
)

var (
	getByTestCases = []TestCase{{
		A: 10,
		B: "hello",
	}, {
		A: 20,
		B: "bye",
	}}
)

func TestGetBy(t *testing.T) {
	t.Run("Test GetBy found", func(t *testing.T) {
		f, err := sliceutils.GetBy(getByTestCases, 10, func(t TestCase) int {
			return t.A
		})
		assert.Equal(t, "hello", f.B)
		assert.NoError(t, err)

		f, err = sliceutils.GetBy(getByTestCases, "bye", func(t TestCase) string {
			return t.B
		})
		assert.Equal(t, 20, f.A)
		assert.NoError(t, err)
	})

	t.Run("Test GetBy not found", func(t *testing.T) {
		_, err := sliceutils.GetBy(getByTestCases, 30, func(t TestCase) int {
			return t.A
		})
		assert.Error(t, err)
	})
}

func TestMustGetBy(t *testing.T) {
	t.Run("Test MustGetBy found", func(t *testing.T) {
		f := sliceutils.MustGetBy(getByTestCases, 10, func(t TestCase) int {
			return t.A
		})
		assert.Equal(t, "hello", f.B)

		f = sliceutils.MustGetBy(getByTestCases, "bye", func(t TestCase) string {
			return t.B
		})
		assert.Equal(t, 20, f.A)
	})

	t.Run("Test MustGetBy not found", func(t *testing.T) {
		assert.Panics(t, func() {
			_ = sliceutils.MustGetBy(getByTestCases, 30, func(t TestCase) int {
				return t.A
			})
		})
	})
}
