package pattern

import (
	"slices"
	"strconv"
	"strings"
)

// Merge collapses entity names into a Slurm hostlist expression. Entities that end in a dash and
// digits are grouped by their prefix and rendered as ranges; the rest pass through verbatim. Entries
// are ordered naturally by their prefix or name, see NaturalCompare.
func Merge(entities []string) string {
	if len(entities) == 0 {
		return ""
	}

	entitiesByPrefix := make(map[string][]string)
	var prefixes []string
	passthrough := make(map[string]struct{})

	for _, entity := range entities {
		prefix, ok := dashedPrefix(entity)
		if !ok {
			passthrough[entity] = struct{}{}
			continue
		}

		if _, ok := entitiesByPrefix[prefix]; !ok {
			prefixes = append(prefixes, prefix)
		}
		entitiesByPrefix[prefix] = append(entitiesByPrefix[prefix], entity)
	}

	parts := make([]keyedPart, 0, len(prefixes)+len(passthrough))
	for _, prefix := range prefixes {
		parts = append(parts, keyedPart{key: prefix, text: MergePrefixed(entitiesByPrefix[prefix], prefix)})
	}
	for entity := range passthrough {
		parts = append(parts, keyedPart{key: entity, text: entity})
	}

	return joinNaturally(parts)
}

// keyedPart is one rendered entry of a hostlist with the text it is ordered by.
type keyedPart struct {
	key  string
	text string
}

func joinNaturally(parts []keyedPart) string {
	slices.SortFunc(parts, func(a, b keyedPart) int {
		return NaturalCompare(a.key, b.key)
	})
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		texts = append(texts, part.text)
	}
	return strings.Join(texts, ",")
}

func MergePrefixed(entities []string, prefix string) string {
	if len(entities) == 0 {
		return ""
	}

	suffixesByWidth := make(map[int][]int)
	seenSuffixes := make(map[suffixKey]struct{}, len(entities))
	var numericSuffixes []numericSuffix
	passthrough := make(map[string]struct{})
	hasPaddedSuffix := false

	for _, entity := range entities {
		if !strings.HasPrefix(entity, prefix) {
			passthrough[entity] = struct{}{}
			continue
		}

		suffix := strings.TrimPrefix(entity, prefix)
		if suffix == "" || !isDigits(suffix) {
			passthrough[entity] = struct{}{}
			continue
		}

		number, err := strconv.Atoi(suffix)
		if err != nil {
			passthrough[entity] = struct{}{}
			continue
		}

		numericSuffixes = append(numericSuffixes, numericSuffix{
			number: number,
			width:  len(suffix),
		})
		hasPaddedSuffix = hasPaddedSuffix || hasLeadingZero(suffix)
	}

	for _, suffix := range numericSuffixes {
		width := 0
		if hasPaddedSuffix {
			width = suffix.width
		}

		key := suffixKey{number: suffix.number, width: width}
		if _, ok := seenSuffixes[key]; ok {
			continue
		}
		seenSuffixes[key] = struct{}{}
		suffixesByWidth[key.width] = append(suffixesByWidth[key.width], key.number)
	}

	groups := make([]suffixGroup, 0, len(suffixesByWidth))
	for width, numbers := range suffixesByWidth {
		slices.Sort(numbers)
		groups = append(groups, suffixGroup{
			width:   width,
			numbers: numbers,
		})
	}
	slices.SortFunc(groups, func(a, b suffixGroup) int {
		if a.numbers[0] < b.numbers[0] {
			return -1
		}
		if a.numbers[0] > b.numbers[0] {
			return 1
		}
		if a.width < b.width {
			return -1
		}
		if a.width > b.width {
			return 1
		}
		return 0
	})

	// Groups share the prefix and are already ordered among themselves, so they keep their place
	// relative to each other by sorting on the prefix alone: SortFunc is stable.
	parts := make([]keyedPart, 0, len(groups)+len(passthrough))
	for _, group := range groups {
		parts = append(parts, keyedPart{key: prefix, text: renderSuffixGroup(prefix, group)})
	}
	for entity := range passthrough {
		parts = append(parts, keyedPart{key: entity, text: entity})
	}

	return joinNaturally(parts)
}
