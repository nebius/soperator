package sharedsteps

import (
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSlurmSettingsTable(t *testing.T) {
	table := newTable(
		[]string{"setting", "value"},
		[]string{" MpiDefault ", " pmix "},
		[]string{"SlurmdTimeout", "180 sec"},
	)

	expected, err := parseSlurmSettingsTable(table)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"MpiDefault":    "pmix",
		"SlurmdTimeout": "180 sec",
	}, expected)
}

func TestParseSlurmSettingsTableRejectsInvalidTables(t *testing.T) {
	for name, table := range map[string]*godog.Table{
		"nil":              nil,
		"empty":            {},
		"wrong header":     newTable([]string{"name", "expected"}, []string{"MpiDefault", "pmix"}),
		"header only":      newTable([]string{"setting", "value"}),
		"wrong cell count": newTable([]string{"setting", "value"}, []string{"MpiDefault"}),
		"empty setting":    newTable([]string{"setting", "value"}, []string{"", "pmix"}),
		"empty value":      newTable([]string{"setting", "value"}, []string{"MpiDefault", ""}),
		"duplicate": newTable(
			[]string{"setting", "value"},
			[]string{"MpiDefault", "pmix"},
			[]string{"MpiDefault", "none"},
		),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseSlurmSettingsTable(table)
			assert.Error(t, err)
		})
	}
}

func TestValidateSlurmSettingsReportsAllProblemsInOrder(t *testing.T) {
	err := validateSlurmSettings(
		map[string]string{"TaskPlugin": "task/none"},
		map[string]string{
			"TaskPlugin": "task/cgroup,task/affinity",
			"MpiDefault": "pmix",
			"JobRequeue": "1",
		},
	)

	require.EqualError(t, err, `validate Slurm settings: JobRequeue: missing, expected "1"; MpiDefault: missing, expected "pmix"; TaskPlugin: expected "task/cgroup,task/affinity", got "task/none"`)
}

func TestValidateSlurmSettingsAcceptsExactValues(t *testing.T) {
	configuration := map[string]string{
		"MpiDefault":    "pmix",
		"SlurmdTimeout": "180 sec",
	}

	assert.NoError(t, validateSlurmSettings(configuration, configuration))
}
