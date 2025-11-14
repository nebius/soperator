package sliceutils

// Filter returns elements from the slice where the matcher is true.
func Filter[T any](slice []T, matcher func(T) bool) []T {
	res := make([]T, 0, len(slice))
	for _, v := range slice {
		if matcher(v) {
			res = append(res, v)
		}
	}

	return res
}
