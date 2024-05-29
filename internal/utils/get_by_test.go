package utils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils"
)

func TestValidateGetBy(t *testing.T) {
	type S struct {
		A int
		B string
	}

	s := []S{{
		A: 10,
		B: "hello",
	}, {
		A: 20,
		B: "bye",
	}}

	t.Run("Test GetBy found", func(t *testing.T) {
		f, err := utils.GetBy(s, 10, func(t S) int {
			return t.A
		})
		assert.Equal(t, "hello", f.B)
		assert.NoError(t, err)

		f, err = utils.GetBy(s, "bye", func(t S) string {
			return t.B
		})
		assert.Equal(t, 20, f.A)
		assert.NoError(t, err)
	})

	t.Run("Test GetBy not found", func(t *testing.T) {
		_, err := utils.GetBy(s, 30, func(t S) int {
			return t.A
		})
		assert.Error(t, err)
	})

	t.Run("Test MustGetBy found", func(t *testing.T) {
		f := utils.MustGetBy(s, 10, func(t S) int {
			return t.A
		})
		assert.Equal(t, "hello", f.B)

		f = utils.MustGetBy(s, "bye", func(t S) string {
			return t.B
		})
		assert.Equal(t, 20, f.A)
	})

	t.Run("Test MustGetBy not found", func(t *testing.T) {
		assert.Panics(t, func() {
			_ = utils.MustGetBy(s, 30, func(t S) int {
				return t.A
			})
		})
	})
}
