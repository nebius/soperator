package framework

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlurmClientConfiguration(t *testing.T) {
	runtime := &slurmConfigurationRuntime{output: `Configuration data as of 2026-09-01T15:00:00
MpiDefault              = pmix
SlurmdTimeout           = 180 sec
PluginOption            = name=value
`}

	configuration, err := NewSlurmClient(runtime).Configuration(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"MpiDefault":    "pmix",
		"PluginOption":  "name=value",
		"SlurmdTimeout": "180 sec",
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

type slurmConfigurationRuntime struct {
	Runtime
	output string
}

func (r *slurmConfigurationRuntime) Controller() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if command != "scontrol show config" {
			return "", fmt.Errorf("unexpected controller command: %s", command)
		}
		return r.output, nil
	})
}
