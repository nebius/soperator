package utils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils"
)

func TestValidateUniqueEntries(t *testing.T) {
	type S struct {
		ID   int
		Name string
	}
	slice := []S{
		{ID: 1, Name: "Alice"},
		{ID: 2, Name: "Bob"},
		{ID: 3, Name: "Charlie"},
		{ID: 2, Name: "David"},
	}

	t.Run("Test Check for duplicates by the 'ID' field", func(t *testing.T) {
		isUnique := utils.ValidateUniqueEntries(
			slice,
			func(s S) int { return s.ID },
		)
		assert.False(t, isUnique)
	})

	t.Run("Test Check for duplicates by the 'Name' field", func(t *testing.T) {
		isUnique := utils.ValidateUniqueEntries(
			slice,
			func(s S) string { return s.Name },
		)
		assert.True(t, isUnique)
	})
}
