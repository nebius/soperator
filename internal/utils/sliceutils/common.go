package sliceutils

import (
	"iter"
)

// Collect collects all elements from the seq into a slice.
func Collect[T any](seq iter.Seq[T]) []T {
	var out []T
	seq(func(v T) bool {
		out = append(out, v)
		return true
	})
	return out
}

// SliceSeq converts a slice to an iter.Seq.
func SliceSeq[T any](s []T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, v := range s {
			if !yield(v) {
				return
			}
		}
	}
}

// IsEmptySeq checks if the given seq is empty.
func IsEmptySeq[T any](seq iter.Seq[T]) bool {
	empty := true
	seq(func(_ T) bool {
		empty = false
		return false // сразу останавливаемся — нам достаточно одного элемента
	})
	return empty
}
