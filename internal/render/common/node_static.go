package common

import (
	"cmp"
	"slices"
	"strings"

	slurmassignment "nebius.ai/slurm-operator/internal/utils/slurm/assignment"
)

const (
	// nodeFeatureKey starts the Feature assignment of a NodeName line.
	nodeFeatureKey = "Feature="
	// nodeStateKey starts the State assignment of a NodeName line.
	nodeStateKey = "State="
	// nodeSuspendTimeKey is the key of the SuspendTime assignment of a NodeName line.
	nodeSuspendTimeKey = "SuspendTime"
)

// filterNodeStatic removes the settings of nodeConfig.static that the rendered NodeName line sets itself: Feature
// and State as in filterNodeStaticFeatureAndState and, with dropSuspendTime, every SuspendTime assignment.
//
// SuspendTime is matched as Slurm parses the line after Feature and State are removed, with the key matched
// case-insensitively and whitespace allowed around "=". Parsing the original line instead would miss a
// SuspendTime that an empty "Feature=" swallows as its value there. Removing an assignment can join the text
// around it into a new SuspendTime, so the line is parsed again until none is left.
//
// When nothing is removed, the line is returned unchanged.
func filterNodeStatic(static string, dropSuspendTime bool) string {
	static = filterNodeStaticFeatureAndState(static)
	if !dropSuspendTime {
		return static
	}

	for {
		var removals []byteSpan
		for a := range slurmassignment.All(static) {
			if strings.EqualFold(a.Key, nodeSuspendTimeKey) {
				removals = append(removals, byteSpan{start: a.Start, end: a.End})
			}
		}
		if len(removals) == 0 {
			return static
		}
		static = removeSpans(static, removals)
	}
}

// filterNodeStaticFeatureAndState removes Feature and State from nodeConfig.static, which the rendered NodeName
// line sets itself.
//
// The matching is kept as it has always been for backward compatibility: every word of the line split on spaces
// that starts with exactly "Feature=" or "State=" is removed as a whole, even if it contains other whitespace.
// Other spellings such as "feature=x" or "Feature = x" have always reached Slurm.
//
// When nothing is removed, the line is returned unchanged.
func filterNodeStaticFeatureAndState(static string) string {
	var removals []byteSpan
	start := 0
	for word := range strings.SplitSeq(static, " ") {
		if strings.HasPrefix(word, nodeFeatureKey) || strings.HasPrefix(word, nodeStateKey) {
			removals = append(removals, byteSpan{start: start, end: start + len(word)})
		}
		start += len(word) + len(" ")
	}
	return removeSpans(static, removals)
}

// byteSpan is the half-open byte range [start, end) of a string.
type byteSpan struct {
	start, end int
}

// removeSpans removes the spans from line, merging overlapping ones. Each removal also takes one whitespace
// separator: the one before it, or the one after it when there is none before or it is already removed. For
// words separated by single spaces, this gives the same result as dropping the words and joining the rest with
// spaces.
func removeSpans(line string, spans []byteSpan) string {
	if len(spans) == 0 {
		return line
	}

	slices.SortFunc(spans, func(a, b byteSpan) int { return cmp.Compare(a.start, b.start) })
	var merged []byteSpan
	for _, span := range spans {
		if last := len(merged) - 1; last >= 0 && span.start <= merged[last].end {
			merged[last].end = max(merged[last].end, span.end)
			continue
		}
		merged = append(merged, span)
	}

	var res strings.Builder
	keptFrom := 0
	for _, span := range merged {
		switch {
		case span.start > keptFrom && isSlurmSpace(line[span.start-1]):
			span.start--
		case span.end < len(line) && isSlurmSpace(line[span.end]):
			span.end++
		}
		res.WriteString(line[keptFrom:span.start])
		keptFrom = span.end
	}
	res.WriteString(line[keptFrom:])
	return res.String()
}

// isSlurmSpace reports whether c belongs to the [[:space:]] class that separates Slurm assignments.
func isSlurmSpace(c byte) bool {
	return strings.IndexByte(" \t\n\v\f\r", c) >= 0
}
