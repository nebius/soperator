package tfrunner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRunner(t *testing.T) {
	t.Run("with defaults", func(t *testing.T) {
		opts := &Options{
			TerraformDir: "/tmp",
		}
		runner, err := NewRunner(opts, t)
		require.NoError(t, err)
		require.NotNil(t, runner)

		assert.Equal(t, DefaultTerraformBinary, runner.options.TerraformBinary)
		assert.Equal(t, DefaultGracefulShutdownTimeout, runner.options.GracefulShutdownTimeout)
	})

	t.Run("with invalid retry pattern", func(t *testing.T) {
		opts := &Options{
			TerraformDir: "/tmp",
			RetryableErrors: map[string]string{
				"[invalid": "bad regex",
			},
		}
		runner, err := NewRunner(opts, t)
		assert.Error(t, err)
		assert.Nil(t, runner)
	})
}

func TestRunnerRun(t *testing.T) {
	// Create a temporary directory with a simple terraform config
	tmpDir := t.TempDir()

	// Create a minimal terraform config to test with
	mainTF := filepath.Join(tmpDir, "main.tf")
	err := os.WriteFile(mainTF, []byte(`
terraform {
  required_version = ">= 1.0"
}

output "test" {
  value = "hello"
}
`), 0644)
	require.NoError(t, err)

	opts := &Options{
		TerraformDir: tmpDir,
		NoColor:      true,
	}

	runner, err := NewRunner(opts, t)
	require.NoError(t, err)

	t.Run("init succeeds", func(t *testing.T) {
		ctx := context.Background()
		result, err := runner.Init(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, result.Combined(), "Terraform has been successfully initialized")
	})
}

func TestRunnerContextCancellation(t *testing.T) {
	// Test that context cancellation sends SIGINT
	opts := &Options{
		TerraformDir:            t.TempDir(),
		TerraformBinary:         "sleep", // Use sleep to simulate long-running command
		GracefulShutdownTimeout: 2 * time.Second,
	}

	runner, err := NewRunner(opts, t)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = runner.Run(ctx, "10") // sleep 10 seconds
	elapsed := time.Since(start)

	// Should exit much faster than 10 seconds due to cancellation
	assert.True(t, elapsed < 5*time.Second, "expected early exit due to cancellation")
}

func TestRunnerOutputCapture(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a terraform config that outputs something
	mainTF := filepath.Join(tmpDir, "main.tf")
	err := os.WriteFile(mainTF, []byte(`
terraform {
  required_version = ">= 1.0"
}

variable "message" {
  type    = string
  default = "test"
}

output "echo" {
  value = var.message
}
`), 0644)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	opts := &Options{
		TerraformDir: tmpDir,
		NoColor:      true,
		Stdout:       &stdout,
		Stderr:       &stderr,
	}

	runner, err := NewRunner(opts, t)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = runner.Init(ctx)
	require.NoError(t, err)

	// Verify output was captured
	assert.Contains(t, stdout.String(), "Terraform has been successfully initialized")
}

func TestOptionsWithDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    *Options
		expected *Options
	}{
		{
			name:  "empty options get defaults",
			input: &Options{},
			expected: &Options{
				TerraformBinary:         DefaultTerraformBinary,
				GracefulShutdownTimeout: DefaultGracefulShutdownTimeout,
				TimeBetweenRetries:      DefaultTimeBetweenRetries,
			},
		},
		{
			name: "custom values preserved",
			input: &Options{
				TerraformBinary:         "/custom/terraform",
				GracefulShutdownTimeout: 60 * time.Second,
				TimeBetweenRetries:      5 * time.Second,
			},
			expected: &Options{
				TerraformBinary:         "/custom/terraform",
				GracefulShutdownTimeout: 60 * time.Second,
				TimeBetweenRetries:      5 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input.WithDefaults()
			assert.Equal(t, tt.expected.TerraformBinary, result.TerraformBinary)
			assert.Equal(t, tt.expected.GracefulShutdownTimeout, result.GracefulShutdownTimeout)
			assert.Equal(t, tt.expected.TimeBetweenRetries, result.TimeBetweenRetries)
		})
	}
}

func TestResultCombined(t *testing.T) {
	result := &Result{
		Stdout: "stdout output",
		Stderr: "stderr output",
	}
	assert.Equal(t, "stdout output\nstderr output", result.Combined())
}
