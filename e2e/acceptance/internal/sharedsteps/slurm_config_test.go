package sharedsteps

import (
	"testing"

	"github.com/cucumber/godog"
	messages "github.com/cucumber/messages/go/v21"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSlurmConfiguration(t *testing.T) {
	configuration, err := parseSlurmConfiguration(`Configuration data as of 2026-09-01T15:00:00
MpiDefault              = pmix
SlurmctldTimeout        = 180 sec
PluginOption            = name=value
`)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"MpiDefault":       "pmix",
		"PluginOption":     "name=value",
		"SlurmctldTimeout": "180 sec",
	}, configuration)
}

func TestParseSlurmConfigurationRejectsInvalidOutput(t *testing.T) {
	for name, output := range map[string]string{
		"no settings":    "Configuration data as of today\n",
		"empty setting":  " = value\n",
		"duplicate name": "MpiDefault = pmix\nMpiDefault = none\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseSlurmConfiguration(output)
			assert.Error(t, err)
		})
	}
}

func TestParseSlurmSettingsTable(t *testing.T) {
	table := newSlurmSettingsTable(
		[]string{"setting", "value"},
		[]string{" MpiDefault ", " pmix "},
		[]string{"SlurmctldTimeout", "180 sec"},
	)

	expected, err := parseSlurmSettingsTable(table)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"MpiDefault":       "pmix",
		"SlurmctldTimeout": "180 sec",
	}, expected)
}

func TestParseSlurmSettingsTableRejectsInvalidTables(t *testing.T) {
	for name, table := range map[string]*godog.Table{
		"nil":              nil,
		"empty":            {},
		"wrong header":     newSlurmSettingsTable([]string{"name", "expected"}, []string{"MpiDefault", "pmix"}),
		"header only":      newSlurmSettingsTable([]string{"setting", "value"}),
		"wrong cell count": newSlurmSettingsTable([]string{"setting", "value"}, []string{"MpiDefault"}),
		"empty setting":    newSlurmSettingsTable([]string{"setting", "value"}, []string{"", "pmix"}),
		"empty value":      newSlurmSettingsTable([]string{"setting", "value"}, []string{"MpiDefault", ""}),
		"duplicate": newSlurmSettingsTable(
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
		"MpiDefault":       "pmix",
		"SlurmctldTimeout": "180 sec",
	}

	assert.NoError(t, validateSlurmSettings(configuration, configuration))
}

func newSlurmSettingsTable(rows ...[]string) *godog.Table {
	table := &godog.Table{}
	for _, values := range rows {
		row := &messages.PickleTableRow{}
		for _, value := range values {
			row.Cells = append(row.Cells, &messages.PickleTableCell{Value: value})
		}
		table.Rows = append(table.Rows, row)
	}
	return table
}
