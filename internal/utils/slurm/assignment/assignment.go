// Package assignment splits Slurm config lines, such as a NodeName record, into key=value assignments the
// way Slurm does.
package assignment

import (
	"iter"
	"regexp"
	"strings"
)

// pattern is keyvalue_pattern from Slurm's src/common/parse_config.c. It is compiled with POSIX
// leftmost-longest semantics like regexec(3), so a quoted value containing whitespace is matched as one value.
var pattern = regexp.MustCompilePOSIX(
	`^[[:space:]]*([[:alnum:]_.]+)[[:space:]]*[-*+/]?=[[:space:]]*("([^"]*)"|[^[:space:]]+)([[:space:]]|$)`,
)

// space is the [[:space:]] class of pattern.
const space = " \t\n\v\f\r"

// Assignment is one key=value assignment of a Slurm config line.
type Assignment struct {
	// Key is the key as written. Slurm matches keys case-insensitively.
	Key string
	// Value is the value without the surrounding quotes of a quoted value.
	Value string
	// Start is the byte offset of the key in the line.
	Start int
	// End is the byte offset just past the value in the line.
	End int
}

// All returns the assignments of a Slurm config line in order. Whitespace may surround "=", a quoted value may
// contain whitespace, and an operator such as "+=" belongs to its assignment.
//
// Slurm rejects a line containing text that is not an assignment. All skips such text up to the next
// whitespace and continues with the rest of the line.
func All(line string) iter.Seq[Assignment] {
	return func(yield func(Assignment) bool) {
		offset := 0
		for offset < len(line) {
			rest := line[offset:]
			match := pattern.FindStringSubmatchIndex(rest)
			if match == nil {
				trimmed := strings.TrimLeft(rest, space)
				end := strings.IndexAny(trimmed, space)
				if trimmed == "" || end < 0 {
					return
				}
				offset += len(rest) - len(trimmed) + end
				continue
			}

			value := rest[match[4]:match[5]]
			if match[6] >= 0 {
				value = rest[match[6]:match[7]]
			}
			if !yield(Assignment{
				Key:   rest[match[2]:match[3]],
				Value: value,
				Start: offset + match[2],
				End:   offset + match[5],
			}) {
				return
			}
			offset += match[5]
		}
	}
}
