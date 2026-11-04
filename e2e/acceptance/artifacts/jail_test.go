package artifacts

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type retryRecordingArgsScope struct {
	attempts int
	delay    time.Duration
	args     []string
}

func (s *retryRecordingArgsScope) Run(context.Context, ...string) (string, error) {
	return "", nil
}

func (s *retryRecordingArgsScope) RunWithRetry(
	_ context.Context,
	attempts int,
	delay time.Duration,
	args ...string,
) (string, error) {
	s.attempts = attempts
	s.delay = delay
	s.args = args
	return "", nil
}

func (s *retryRecordingArgsScope) RunWithDefaultRetry(context.Context, ...string) (string, error) {
	return "", nil
}

func TestJailCopyUsesRetries(t *testing.T) {
	scope := &retryRecordingArgsScope{}
	collector := &jailCollector{kubectl: scope}
	destination := t.TempDir()

	err := collector.copyFromPod(t.Context(), "worker-0", "slurmd", "/source/.", destination)
	require.NoError(t, err)
	assert.Equal(t, jailCopyAttempts, scope.attempts)
	assert.Equal(t, jailCopyDelay, scope.delay)
	assert.Equal(t, []string{
		"cp", "-c", "slurmd", "soperator/worker-0:/source/.", destination,
	}, scope.args)
}
