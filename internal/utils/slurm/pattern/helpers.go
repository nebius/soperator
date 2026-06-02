package pattern

import (
	"fmt"
	"strings"
)

type suffixKey struct {
	number int
	width  int
}
type numericSuffix struct {
	number int
	width  int
}

type suffixGroup struct {
	width   int
	numbers []int
}

func renderSuffixGroup(prefix string, group suffixGroup) string {
	if len(group.numbers) == 1 {
		return prefix + formatPatternNumber(group.numbers[0], group.width)
	}

	ranges := make([]string, 0, len(group.numbers))
	for i := 0; i < len(group.numbers); i++ {
		start := group.numbers[i]
		end := start
		for i+1 < len(group.numbers) && group.numbers[i+1] == end+1 {
			i++
			end = group.numbers[i]
		}

		if start == end {
			ranges = append(ranges, formatPatternNumber(start, group.width))
			continue
		}

		ranges = append(
			ranges,
			formatPatternNumber(start, group.width)+"-"+formatPatternNumber(end, group.width),
		)
	}

	return fmt.Sprintf("%s[%s]", prefix, strings.Join(ranges, ","))
}

func formatPatternNumber(number, width int) string {
	return fmt.Sprintf("%0*d", width, number)
}

func hasLeadingZero(s string) bool {
	return len(s) > 1 && s[0] == '0'
}

func dashedPrefix(s string) (string, bool) {
	i := strings.LastIndex(s, "-")
	if i == -1 || i == len(s)-1 {
		return "", false
	}

	suffix := s[i+1:]
	if !isDigits(suffix) {
		return "", false
	}

	return s[:i+1], true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
