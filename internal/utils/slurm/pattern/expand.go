package pattern

import (
	"strconv"
	"strings"
)

// Expand is the inverse of Merge: it turns a merged hostlist expression back into the individual
// entity names it stands for.
//
// Anything it cannot read as a range -- an entity Merge passed through untouched, or a
// hand-written expression -- comes back as itself, so a caller counting entities never loses one to
// a parse it did not expect.
func Expand(merged string) []string {
	if strings.TrimSpace(merged) == "" {
		return nil
	}

	var entities []string
	for _, part := range splitTopLevel(merged) {
		entities = append(entities, expandPart(part)...)
	}
	return entities
}

// splitTopLevel splits on the commas separating entities, leaving the ones inside a range
// expression alone.
func splitTopLevel(merged string) []string {
	var parts []string
	var current strings.Builder
	depth := 0

	flush := func() {
		if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
			parts = append(parts, trimmed)
		}
		current.Reset()
	}

	for _, char := range merged {
		switch {
		case char == '[':
			depth++
		case char == ']':
			depth--
		case char == ',' && depth == 0:
			flush()
			continue
		}
		current.WriteRune(char)
	}
	flush()

	return parts
}

func expandPart(part string) []string {
	open := strings.Index(part, "[")
	if open == -1 || !strings.HasSuffix(part, "]") {
		return []string{part}
	}

	prefix := part[:open]
	ranges := part[open+1 : len(part)-1]

	var entities []string
	for _, item := range strings.Split(ranges, ",") {
		item = strings.TrimSpace(item)
		start, end, width, ok := parseRangeItem(item)
		if !ok {
			return []string{part}
		}
		for number := start; number <= end; number++ {
			entities = append(entities, prefix+formatPatternNumber(number, width))
		}
	}

	return entities
}

func parseRangeItem(item string) (start, end, width int, ok bool) {
	bounds := strings.SplitN(item, "-", 2)

	start, err := strconv.Atoi(bounds[0])
	if err != nil {
		return 0, 0, 0, false
	}
	end = start

	if len(bounds) == 2 {
		end, err = strconv.Atoi(bounds[1])
		if err != nil || end < start {
			return 0, 0, 0, false
		}
	}

	return start, end, len(bounds[0]), true
}
