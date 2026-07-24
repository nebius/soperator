package utils

import (
	"cmp"
)

// ValidateUniqueEntries checks if there are no duplicate values for a field in a slice of structs.
// Returns true if all entries are unique by the value taken via getter. Otherwise, false.
func ValidateUniqueEntries[T any, V cmp.Ordered](slice []T, getter func(T) V) bool {
	seen := map[V]bool{}

	for _, item := range slice {
		fieldValue := getter(item)
		if seen[fieldValue] {
			return false
		}
		seen[fieldValue] = true
	}

	return true
}
