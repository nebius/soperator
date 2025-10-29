package sliceutils

// Map applies f to each alement of the slice, and returns new slice containing processed elements.
func Map[T any, U any](slice []T, f func(T) U) []U {
	result := make([]U, len(slice))
	for i := 0; i < len(slice); i++ {
		result[i] = f(slice[i])
	}
	return result
}
