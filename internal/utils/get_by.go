package utils

import (
	"fmt"

	"golang.org/x/exp/constraints"
)

func GetBy[T any, V constraints.Ordered](slice []T, value V, getter func(T) V) (T, error) {
	for _, v := range slice {
		if getter(v) == value {
			return v, nil
		}
	}
	return *new(T), fmt.Errorf("element with value \"%v\" not found", value)
}

func MustGetBy[T any, V constraints.Ordered](slice []T, value V, getter func(T) V) T {
	for _, v := range slice {
		if getter(v) == value {
			return v
		}
	}
	panic(fmt.Sprintf("value with value \"%v\" not found", value))
}
