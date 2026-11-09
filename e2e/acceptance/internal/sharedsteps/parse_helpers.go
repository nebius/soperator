package sharedsteps

import (
	"regexp"
	"strings"
)

// keyValueLinePattern matches a `key=value` line printed by a test script: an identifier key
// and a non-empty value, so that stray output such as warnings does not register keys.
var keyValueLinePattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.+)$`)

// parseKeyValueLine returns the value of the first key=value line for key, or "" without one.
func parseKeyValueLine(output, key string) string {
	return parseKeyValues(output)[key]
}

// parseKeyValues maps every key=value line of output; the first line of a key wins.
func parseKeyValues(output string) map[string]string {
	values := map[string]string{}
	for line := range strings.SplitSeq(output, "\n") {
		match := keyValueLinePattern.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		if _, seen := values[match[1]]; !seen {
			values[match[1]] = strings.TrimSpace(match[2])
		}
	}
	return values
}

// parsePipeRows splits `--parsable2`-style output into trimmed fields per non-empty line,
// dropping the empty field a trailing separator leaves.
func parsePipeRows(output string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) > 0 && fields[len(fields)-1] == "" {
			fields = fields[:len(fields)-1]
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		rows = append(rows, fields)
	}
	return rows
}

// extractField returns the value of a `Field=value` token in scontrol one-liner output, or "".
func extractField(line, field string) string {
	prefix := field + "="
	for _, token := range strings.Fields(line) {
		if strings.HasPrefix(token, prefix) {
			return strings.TrimPrefix(token, prefix)
		}
	}
	return ""
}
