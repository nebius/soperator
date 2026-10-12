package framework

import (
	"context"
	"fmt"
	"strings"
)

func (s *SlurmClient) Configuration(ctx context.Context) (map[string]string, error) {
	output, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "scontrol show config")
	if err != nil {
		return nil, fmt.Errorf("read effective Slurm configuration: %w", err)
	}

	configuration, err := parseSlurmConfiguration(output)
	if err != nil {
		return nil, fmt.Errorf("parse effective Slurm configuration: %w", err)
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
