package utils

import (
	"golang.org/x/exp/constraints"

	"nebius.ai/slurm-operator/internal/utils/sliceutils"
)

// GetBy finds an element in the slice if its value obtained by getter equals to value.
//
// Deprecated. Use sliceutils.GetBy instead.
func GetBy[T any, V constraints.Ordered](slice []T, value V, getter func(T) V) (T, error) {
	return sliceutils.GetBy(slice, value, getter)
}

// MustGetBy finds an element in the slice if its value obtained by getter equals to value.
// Panics if an element couldn't be found.
//
// Deprecated. Use sliceutils.GetBy instead.
func MustGetBy[T any, V constraints.Ordered](slice []T, value V, getter func(T) V) T {
	return sliceutils.MustGetBy(slice, value, getter)
}
