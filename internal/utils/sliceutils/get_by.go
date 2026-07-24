package sliceutils

import (
	"fmt"

	"cmp"
)

// GetBy finds an element in the slice if its value obtained by getter equals to value.
func GetBy[T any, V cmp.Ordered](slice []T, value V, getter func(T) V) (T, error) {
	for _, v := range slice {
		if getter(v) == value {
			return v, nil
		}
	}
	return *new(T), fmt.Errorf("element with value \"%v\" not found", value)
}

// MustGetBy finds an element in the slice if its value obtained by getter equals to value.
// Panics if an element couldn't be found.
func MustGetBy[T any, V cmp.Ordered](slice []T, value V, getter func(T) V) T {
	for _, v := range slice {
		if getter(v) == value {
			return v
		}
	}
	panic(fmt.Sprintf("element with value \"%v\" not found", value))
}
