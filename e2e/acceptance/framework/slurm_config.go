package framework

import (
	"context"
	"fmt"
	"strings"
)

// SlurmConfigurationError distinguishes invalid configuration or parsed output
// from transient command errors.
type SlurmConfigurationError struct {
	err error
}

// SlurmSuspendExclusions contains only unconditional node exclusions in Names.
// Counted exclusions do not protect particular nodes and prevent incremental updates.
type SlurmSuspendExclusions struct {
	Names   map[string]bool
	Counted bool
}

func (e SlurmConfigurationError) Error() string { return e.err.Error() }
func (e SlurmConfigurationError) Unwrap() error { return e.err }

func (s *SlurmClient) Configuration(ctx context.Context) (map[string]string, error) {
	output, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "scontrol show config")
	if err != nil {
		return nil, fmt.Errorf("read effective Slurm configuration: %w", err)
	}

	configuration, err := parseSlurmConfiguration(output)
	if err != nil {
		return nil, SlurmConfigurationError{err: fmt.Errorf("parse effective Slurm configuration: %w", err)}
	}
	return configuration, nil
}

func parseSlurmConfiguration(output string) (map[string]string, error) {
	configuration := make(map[string]string)
	for lineNumber, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return nil, fmt.Errorf("parse line %d: setting name is empty", lineNumber+1)
		}
		if _, found := configuration[key]; found {
			return nil, fmt.Errorf("parse line %d: setting %q is duplicated", lineNumber+1, key)
		}
		configuration[key] = value
	}
	if len(configuration) == 0 {
		return nil, fmt.Errorf("find settings in scontrol output")
	}
	return configuration, nil
}

func (s *SlurmClient) SuspendExclusions(ctx context.Context) (SlurmSuspendExclusions, error) {
	result := SlurmSuspendExclusions{Names: make(map[string]bool)}
	configuration, err := s.Configuration(ctx)
	if err != nil {
		return result, err
	}
	expression, found := configuration["SuspendExcNodes"]
	if !found {
		return result, SlurmConfigurationError{err: fmt.Errorf("read Slurm configuration: missing SuspendExcNodes")}
	}
	if expression == "(null)" {
		expression = ""
	}
	terms, err := splitSuspendExclusions(expression)
	if err != nil {
		return result, SlurmConfigurationError{err: fmt.Errorf("parse SuspendExcNodes: %w", err)}
	}
	var unconditional []string
	for _, term := range terms {
		if strings.Contains(term, ":") {
			result.Counted = true
			continue
		}
		unconditional = append(unconditional, term)
	}
	if len(unconditional) == 0 {
		return result, nil
	}
	output, err := s.runtime.Controller().RunWithDefaultRetry(ctx,
		"scontrol show hostnames "+ShellQuote(strings.Join(unconditional, ",")))
	if err != nil {
		return result, fmt.Errorf("expand SuspendExcNodes hostlist: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		name := strings.TrimSpace(line)
		if !IsSlurmNodeName(name) {
			return result, SlurmConfigurationError{err: fmt.Errorf("expand SuspendExcNodes: invalid node name %q", name)}
		}
		result.Names[name] = true
	}
	return result, nil
}

// AddSuspendExclusions requires SuspendExcNodes to have no counted exclusions.
func (s *SlurmClient) AddSuspendExclusions(ctx context.Context, names []string) error {
	return s.updateSuspendExclusions(ctx, "+=", names)
}

// RemoveSuspendExclusions requires SuspendExcNodes to have no counted exclusions.
func (s *SlurmClient) RemoveSuspendExclusions(ctx context.Context, names []string) error {
	return s.updateSuspendExclusions(ctx, "-=", names)
}

func (s *SlurmClient) updateSuspendExclusions(ctx context.Context, operator string, names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("update SuspendExcNodes: empty node set")
	}
	for _, name := range names {
		if !IsSlurmNodeName(name) {
			return fmt.Errorf("update SuspendExcNodes: invalid node name %q", name)
		}
	}
	// An uncertain write may have applied; the caller owns reconciliation.
	_, err := s.runtime.Controller().Run(ctx, "scontrol update "+ShellQuote("SuspendExcNodes"+operator+strings.Join(names, ",")))
	return err
}

// A hostlist may contain commas inside brackets. Counted ranges are kept intact
// so they are never mistaken for unconditional protection of individual nodes.
func splitSuspendExclusions(expression string) ([]string, error) {
	if expression == "" {
		return nil, nil
	}
	var terms []string
	depth, start := 0, 0
	for i, r := range expression {
		switch r {
		case '[':
			if depth != 0 {
				return nil, fmt.Errorf("nested brackets in exclusion hostlist")
			}
			depth++
		case ']':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unmatched bracket in exclusion hostlist")
			}
		case ',':
			if depth == 0 {
				terms = append(terms, expression[start:i])
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("unclosed bracket in exclusion hostlist")
	}
	terms = append(terms, expression[start:])
	for i, term := range terms {
		terms[i] = strings.TrimSpace(term)
		if terms[i] == "" {
			return nil, fmt.Errorf("empty entry in exclusion hostlist")
		}
	}
	return terms, nil
}

// IsSlurmNodeName accepts a single literal node name, not a hostlist expression.
func IsSlurmNodeName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
