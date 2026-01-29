package tfrunner

import (
	"io"
	"time"
)

const (
	DefaultGracefulShutdownTimeout = 120 * time.Second
	DefaultTimeBetweenRetries      = 10 * time.Second
	DefaultTerraformBinary         = "terraform"
)

// Options configures the Terraform runner.
type Options struct {
	// TerraformDir is the directory containing terraform configuration files.
	TerraformDir string

	// TerraformBinary is the path to the terraform binary. Defaults to "terraform".
	TerraformBinary string

	// Vars are the terraform variables to pass via -var flags.
	Vars map[string]any

	// EnvVars are environment variables to set when running terraform.
	EnvVars map[string]string

	// RetryableErrors maps regex patterns to descriptions of retryable errors.
	// When terraform output matches a pattern, the command will be retried.
	RetryableErrors map[string]string

	// MaxRetries is the maximum number of retries for retryable errors.
	MaxRetries int

	// TimeBetweenRetries is the duration to wait between retries.
	TimeBetweenRetries time.Duration

	// NoColor disables color output from terraform.
	NoColor bool

	// GracefulShutdownTimeout is the duration to wait for terraform to exit gracefully
	// after receiving SIGINT before sending SIGKILL.
	GracefulShutdownTimeout time.Duration

	// Stdout is the writer for terraform stdout. If nil, os.Stdout is used.
	Stdout io.Writer

	// Stderr is the writer for terraform stderr. If nil, os.Stderr is used.
	Stderr io.Writer
}

// WithDefaults returns a copy of options with default values applied for unset fields.
func (o *Options) WithDefaults() *Options {
	opts := *o
	if opts.TerraformBinary == "" {
		opts.TerraformBinary = DefaultTerraformBinary
	}
	if opts.GracefulShutdownTimeout == 0 {
		opts.GracefulShutdownTimeout = DefaultGracefulShutdownTimeout
	}
	if opts.TimeBetweenRetries == 0 {
		opts.TimeBetweenRetries = DefaultTimeBetweenRetries
	}
	return &opts
}
