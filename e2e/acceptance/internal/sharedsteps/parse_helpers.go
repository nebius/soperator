package sharedsteps

import (
	"regexp"
	"strings"
)

var keyValueLinePattern = regexp.MustCompile(`(?m)^[\t ]*([A-Z_]+)=(.+)$`)

func parseKeyValueLine(output, key string) string {
	for _, match := range keyValueLinePattern.FindAllStringSubmatch(output, -1) {
		if len(match) == 3 && match[1] == key {
			return strings.TrimSpace(match[2])
		}
	}
	return ""
}
