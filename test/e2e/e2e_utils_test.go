//go:build e2e

package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/stretchr/testify/require"
)

// setupTerraformOptions creates common terraform options for e2e tests
func setupTerraformOptions(t *testing.T, cfg testConfig) terraform.Options {
	tfVars := readTFVars(t, fmt.Sprintf("%s/terraform.tfvars", cfg.PathToInstallation))
	tfVars = overrideTestValues(t, tfVars, cfg)

	envVarsList := os.Environ()
	envVars := make(map[string]string)
	for _, envVar := range envVarsList {
		pair := strings.SplitN(envVar, "=", 2)
		require.Len(t, pair, 2)
		envVars[pair[0]] = pair[1]
	}

	return terraform.Options{
		TerraformDir: cfg.PathToInstallation,
		Vars:         tfVars,
		EnvVars:      envVars,
		NoColor:      true,
	}
}

func ensureOutputFiles(t *testing.T, cfg testConfig) {
	ensureFile(t, cfg.OutputLogFile)
	ensureFile(t, cfg.OutputErrFile)
}

func ensureFile(t *testing.T, filename string) {
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0755))

	// Use O_CREATE without O_TRUNC to create file if not exists, but don't truncate if exists
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func writeOutputs(t *testing.T, cfg testConfig, testPhase, command, output string, err error) {
	writeOutput(t, cfg.OutputLogFile, testPhase, command, output)
	if err != nil {
		writeOutput(t, cfg.OutputErrFile, testPhase, command, err.Error())
	}
}

func writeOutput(t *testing.T, filename, testPhase, command, data string) {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer f.Close()

	// Write phase header with timestamp
	_, err = f.WriteString(fmt.Sprintf("========================================\n"))
	require.NoError(t, err)
	_, err = f.WriteString(fmt.Sprintf("Test Phase: %s\n", testPhase))
	require.NoError(t, err)
	_, err = f.WriteString(fmt.Sprintf("Timestamp: %s\n", time.Now().Format("2006-01-02 15:04:05 MST")))
	require.NoError(t, err)
	_, err = f.WriteString(fmt.Sprintf("========================================\n\n"))
	require.NoError(t, err)

	_, err = f.WriteString(fmt.Sprintf("Executing %s\n\n", command))
	require.NoError(t, err)

	_, err = f.WriteString(fmt.Sprintf("%s\n\n", data))
	require.NoError(t, err)
}
