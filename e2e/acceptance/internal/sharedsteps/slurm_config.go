package sharedsteps

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type SlurmConfig struct {
	info          *framework.ClusterInfo
	runtime       framework.Runtime
	configuration map[string]string
}

func NewSlurmConfig(info *framework.ClusterInfo, runtime framework.Runtime) *SlurmConfig {
	return &SlurmConfig{info: info, runtime: runtime}
}

func (s *SlurmConfig) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the effective Slurm configuration is read$`, s.readEffectiveSlurmConfiguration)
	sc.Step(`^it contains the following settings:$`, s.checkEffectiveSlurmSettings)
	sc.Step(`^its cluster name matches the target cluster$`, s.checkClusterName)
}

func (s *SlurmConfig) CleanupAndReset(ctx context.Context) {
	s.configuration = nil
}

func (s *SlurmConfig) readEffectiveSlurmConfiguration(ctx context.Context) error {
	output, err := s.runtime.Controller().RunWithDefaultRetry(ctx, "scontrol show config")
	if err != nil {
		return fmt.Errorf("read effective Slurm configuration: %w", err)
	}

	configuration, err := parseSlurmConfiguration(output)
	if err != nil {
		return fmt.Errorf("parse effective Slurm configuration: %w", err)
	}
	s.configuration = configuration
	return nil
}

func (s *SlurmConfig) checkEffectiveSlurmSettings(table *godog.Table) error {
	if s.configuration == nil {
		return fmt.Errorf("read effective Slurm configuration before validating settings")
	}

	expected, err := parseSlurmSettingsTable(table)
	if err != nil {
		return fmt.Errorf("parse expected Slurm settings: %w", err)
	}

	return validateSlurmSettings(s.configuration, expected)
}

func (s *SlurmConfig) checkClusterName() error {
	if s.configuration == nil {
		return fmt.Errorf("read effective Slurm configuration before validating ClusterName")
	}

	return validateSlurmSettings(s.configuration, map[string]string{
		"ClusterName": s.info.SlurmClusterName,
	})
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

func parseSlurmSettingsTable(table *godog.Table) (map[string]string, error) {
	if table == nil || len(table.Rows) == 0 {
		return nil, fmt.Errorf("find table rows")
	}
	if len(table.Rows[0].Cells) != 2 || table.Rows[0].Cells[0].Value != "setting" || table.Rows[0].Cells[1].Value != "value" {
		return nil, fmt.Errorf(`use the header "setting | value"`)
	}
	if len(table.Rows) == 1 {
		return nil, fmt.Errorf("find settings below the table header")
	}

	expected := make(map[string]string, len(table.Rows)-1)
	for rowNumber, row := range table.Rows[1:] {
		if len(row.Cells) != 2 {
			return nil, fmt.Errorf("parse row %d: expected 2 cells, got %d", rowNumber+2, len(row.Cells))
		}
		setting := strings.TrimSpace(row.Cells[0].Value)
		value := strings.TrimSpace(row.Cells[1].Value)
		if setting == "" {
			return nil, fmt.Errorf("parse row %d: setting name is empty", rowNumber+2)
		}
		if value == "" {
			return nil, fmt.Errorf("parse row %d: value for %q is empty", rowNumber+2, setting)
		}
		if _, found := expected[setting]; found {
			return nil, fmt.Errorf("parse row %d: setting %q is duplicated", rowNumber+2, setting)
		}
		expected[setting] = value
	}
	return expected, nil
}

func validateSlurmSettings(configuration, expected map[string]string) error {
	settings := make([]string, 0, len(expected))
	for setting := range expected {
		settings = append(settings, setting)
	}
	sort.Strings(settings)

	var problems []string
	for _, setting := range settings {
		actual, found := configuration[setting]
		if !found {
			problems = append(problems, fmt.Sprintf("%s: missing, expected %q", setting, expected[setting]))
			continue
		}
		if actual != expected[setting] {
			problems = append(problems, fmt.Sprintf("%s: expected %q, got %q", setting, expected[setting], actual))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("validate Slurm settings: %s", strings.Join(problems, "; "))
	}
	return nil
}
