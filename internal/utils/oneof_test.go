package utils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils"
)

func TestValidateOneOf(t *testing.T) {
	type S struct {
		A *int
		B *string
	}

	var (
		a = 10
		b = "hello"
	)

	t.Run("Test one of specified", func(t *testing.T) {
		s1 := utils.ValidateOneOf(S{A: &a})
		s2 := utils.ValidateOneOf(S{B: &b})
		assert.True(t, s1)
		assert.True(t, s2)
	})

	t.Run("Test multiple specified", func(t *testing.T) {
		s := utils.ValidateOneOf(S{A: &a, B: &b})
		assert.False(t, s)
	})

	t.Run("Test no specified", func(t *testing.T) {
		s := utils.ValidateOneOf(S{})
		assert.False(t, s)
	})
}
