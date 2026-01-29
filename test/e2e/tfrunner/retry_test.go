package tfrunner

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileRetryPatterns(t *testing.T) {
	tests := []struct {
		name        string
		patterns    map[string]string
		expectError bool
	}{
		{
			name:        "empty patterns",
			patterns:    map[string]string{},
			expectError: false,
		},
		{
			name: "valid patterns",
			patterns: map[string]string{
				"(?m)^.*context deadline exceeded.*$": "retry on context deadline exceeded",
				"connection reset by peer":            "retry on connection reset",
			},
			expectError: false,
		},
		{
			name: "invalid pattern",
			patterns: map[string]string{
				"[invalid": "bad regex",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := compileRetryPatterns(tt.patterns)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, compiled)
			} else {
				assert.NoError(t, err)
				assert.Len(t, compiled, len(tt.patterns))
			}
		})
	}
}

func TestMatchRetryableError(t *testing.T) {
	patterns := map[string]string{
		"(?m)^.*context deadline exceeded.*$": "deadline exceeded",
		"connection reset by peer":            "connection reset",
		"etcdserver: leader changed":          "leader changed",
	}

	compiled, err := compileRetryPatterns(patterns)
	require.NoError(t, err)

	tests := []struct {
		name          string
		stdout        string
		stderr        string
		err           error
		expectedMatch string
	}{
		{
			name:          "no match",
			stdout:        "everything is fine",
			stderr:        "",
			err:           nil,
			expectedMatch: "",
		},
		{
			name:          "match in stdout",
			stdout:        "Error: context deadline exceeded",
			stderr:        "",
			err:           nil,
			expectedMatch: "deadline exceeded",
		},
		{
			name:          "match in stderr",
			stdout:        "",
			stderr:        "connection reset by peer",
			err:           nil,
			expectedMatch: "connection reset",
		},
		{
			name:          "match in error",
			stdout:        "",
			stderr:        "",
			err:           errors.New("etcdserver: leader changed"),
			expectedMatch: "leader changed",
		},
		{
			name:          "match in combined output",
			stdout:        "some output",
			stderr:        "etcdserver: leader changed during operation",
			err:           nil,
			expectedMatch: "leader changed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := matchRetryableError(compiled, tt.stdout, tt.stderr, tt.err)
			assert.Equal(t, tt.expectedMatch, match)
		})
	}
}
