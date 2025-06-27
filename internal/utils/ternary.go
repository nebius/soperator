package utils

// Ternary is a small function implementing ternary expression to achieve easier to read one-liners.
func Ternary[T any](condition bool, a T, b T) T {
	if condition {
		return a
	}

	return b
}
