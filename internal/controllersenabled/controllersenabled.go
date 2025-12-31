package controllersenabled

import (
	"fmt"
	"strings"
)

type Set struct {
	enabled map[string]bool
}

func New(spec string, available []string) (*Set, error) {
	enabled := make(map[string]bool, len(available))
	all := make(map[string]struct{}, len(available))
	for _, name := range available {
		lower := strings.ToLower(name)
		all[lower] = struct{}{}
		enabled[lower] = false
	}

	if spec == "" {
		for name := range all {
			enabled[name] = true
		}
		return &Set{enabled: enabled}, nil
	}

	entries := strings.Split(spec, ",")
	hasWildcard := false
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if entry == "*" {
			hasWildcard = true
			break
		}
	}
	if hasWildcard {
		for name := range all {
			enabled[name] = true
		}
	}

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" || entry == "*" {
			continue
		}
		disable := strings.HasPrefix(entry, "-")
		name := strings.ToLower(strings.TrimPrefix(entry, "-"))
		if _, ok := all[name]; !ok {
			return nil, fmt.Errorf("unknown controller %q in SLURM_OPERATOR_CONTROLLERS", entry)
		}
		enabled[name] = !disable
	}

	return &Set{enabled: enabled}, nil
}

func (s *Set) Enabled(name string) bool {
	if s == nil {
		return false
	}
	return s.enabled[strings.ToLower(name)]
}
