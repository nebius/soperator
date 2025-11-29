package sliceutils

import (
	"iter"
)

// Map applies f to each element of the slice, and returns new slice containing processed elements.
// Deprecated: use MapSlice instead.
func Map[T any, U any](slice []T, f func(T) U) []U {
	result := make([]U, len(slice))
	for i := 0; i < len(slice); i++ {
		result[i] = f(slice[i])
	}
	return result
}

// MapSlice applies f to each element of the slice, and returns new slice containing processed elements.
func MapSlice[T any, U any](slice []T, f func(T) U) []U {
	return Collect(MapSeq(SliceSeq(slice), f))
}

// MapSliceSeq applies f to each element of the slice, and returns new slice containing processed elements.
func MapSliceSeq[T any, U any](slice []T, f func(T) U) iter.Seq[U] {
	return MapSeq(SliceSeq(slice), f)
}

// MapSeqSlice applies f to each element of the slice, and returns new slice containing processed elements.
func MapSeqSlice[T any, U any](seq iter.Seq[T], f func(T) U) []U {
	return Collect(MapSeq(seq, f))
}

// MapSeq applies f to each element of the slice, and returns new slice containing processed elements.
func MapSeq[T any, U any](seq iter.Seq[T], f func(T) U) iter.Seq[U] {
	return func(yield func(U) bool) {
		seq(func(v T) bool {
			return yield(f(v))
		})
	}
}
