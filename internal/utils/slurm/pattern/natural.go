package pattern

import (
	"cmp"
	"strings"
)

// NaturalCompare orders strings the way a person reads names with numbers in them: runs of digits
// compare by value, so "rack2" sorts before "rack10", and everything else compares byte-wise. Two
// digit runs of equal value but different padding fall back to their text, so the order is total.
func NaturalCompare(a, b string) int {
	for a != "" && b != "" {
		digitsA, restA := splitLeadingDigits(a)
		digitsB, restB := splitLeadingDigits(b)
		if digitsA == "" || digitsB == "" {
			if a[0] != b[0] {
				return cmp.Compare(a[0], b[0])
			}
			a, b = a[1:], b[1:]
			continue
		}
		if c := compareDigitRuns(digitsA, digitsB); c != 0 {
			return c
		}
		a, b = restA, restB
	}
	return cmp.Compare(len(a), len(b))
}

func splitLeadingDigits(s string) (digits, rest string) {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	return s[:end], s[end:]
}

func compareDigitRuns(a, b string) int {
	trimmedA := strings.TrimLeft(a, "0")
	trimmedB := strings.TrimLeft(b, "0")
	return cmp.Or(
		cmp.Compare(len(trimmedA), len(trimmedB)),
		strings.Compare(trimmedA, trimmedB),
		strings.Compare(a, b),
	)
}
