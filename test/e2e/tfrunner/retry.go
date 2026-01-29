package tfrunner

import (
	"regexp"
	"strings"
)

// compiledPattern holds a compiled regex pattern and its description.
type compiledPattern struct {
	pattern     *regexp.Regexp
	description string
}

// compileRetryPatterns compiles the regex patterns from RetryableErrors map.
// Returns an error if any pattern fails to compile.
func compileRetryPatterns(patterns map[string]string) ([]compiledPattern, error) {
	var compiled []compiledPattern
	for pattern, description := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, compiledPattern{
			pattern:     re,
			description: description,
		})
	}
	return compiled, nil
}

// matchRetryableError checks if the given output matches any of the retryable error patterns.
// Returns the description of the matched pattern, or empty string if no match.
func matchRetryableError(patterns []compiledPattern, stdout, stderr string, err error) string {
	combined := stdout + "\n" + stderr
	if err != nil {
		combined += "\n" + err.Error()
	}
	combined = strings.TrimSpace(combined)

	for _, p := range patterns {
		if p.pattern.MatchString(combined) {
			return p.description
		}
	}
	return ""
}
