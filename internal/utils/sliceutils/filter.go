package sliceutils

import (
	"iter"
)

// Filter returns elements from the slice where the matcher is true.
// Deprecated: use FilterSlice instead.
func Filter[T any](slice []T, matcher func(T) bool) []T {
	res := make([]T, 0, len(slice))
	for _, v := range slice {
		if matcher(v) {
			res = append(res, v)
		}
	}

	return res
}

// FilterSlice returns elements from the slice where the matcher is true.
func FilterSlice[T any](slice []T, matcher func(T) bool) []T {
	return Collect(FilterSeq(SliceSeq(slice), matcher))
}

// FilterSliceSeq returns elements from the slice where the matcher is true.
func FilterSliceSeq[T any](slice []T, matcher func(T) bool) iter.Seq[T] {
	return FilterSeq(SliceSeq(slice), matcher)
}

// FilterSeqSlice returns elements from the slice where the matcher is true.
func FilterSeqSlice[T any](seq iter.Seq[T], matcher func(T) bool) []T {
	return Collect(FilterSeq(seq, matcher))
}

// FilterSeq returns elements from the slice where the matcher is true.
func FilterSeq[T any](seq iter.Seq[T], matcher func(T) bool) iter.Seq[T] {
	return func(yield func(T) bool) {
		seq(func(v T) bool {
			if !matcher(v) {
				return true
			}
			return yield(v)
		})
	}
}
