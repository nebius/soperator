package utils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils"
)

func TestTernary(t *testing.T) {
	t.Run("Test condition is true", func(t *testing.T) {
		assert.Equal(t, 1, utils.Ternary[int](true, 1, 0))
	})

	t.Run("Test condition is false", func(t *testing.T) {
		assert.Equal(t, 0, utils.Ternary[int](false, 1, 0))
	})
}
