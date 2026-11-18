package sharedsteps

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
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

// parseSlurmSeconds reads scontrol's "<N> sec" duration format, and "NONE" that it prints for zero.
func parseSlurmSeconds(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "NONE" {
		return 0, nil
	}
	value, unit, found := strings.Cut(raw, " ")
	if !found || strings.TrimSpace(unit) != "sec" {
		return 0, fmt.Errorf("expected \"<seconds> sec\"")
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return 0, fmt.Errorf("expected a non-negative integer number of seconds")
	}
	return time.Duration(seconds) * time.Second, nil
}
