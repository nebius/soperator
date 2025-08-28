package tokenstandalone

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

func TestNewStandaloneTokenIssuer(t *testing.T) {
	clusterID := types.NamespacedName{Namespace: "test", Name: "cluster"}
	username := "testuser"

	issuer := NewStandaloneTokenIssuer(clusterID, username)

	if issuer == nil {
		t.Fatal("Expected non-nil issuer")
	}

	if issuer.GetClusterID() != clusterID {
		t.Errorf("Expected clusterID %v, got %v", clusterID, issuer.GetClusterID())
	}

	if issuer.GetUsername() != username {
		t.Errorf("Expected username %s, got %s", username, issuer.GetUsername())
	}

	if issuer.GetScontrolPath() != DefaultScontrolPath {
		t.Errorf("Expected default scontrolPath %s, got %s", DefaultScontrolPath, issuer.GetScontrolPath())
	}

	if issuer.GetRotationInterval() != DefaultRotationInterval {
		t.Errorf("Expected default rotationInterval %v, got %v", DefaultRotationInterval, issuer.GetRotationInterval())
	}
}

func TestNewStandaloneTokenIssuer_DefaultUsername(t *testing.T) {
	clusterID := types.NamespacedName{Namespace: "test", Name: "cluster"}

	issuer := NewStandaloneTokenIssuer(clusterID, "")

	if issuer.GetUsername() != DefaultUsername {
		t.Errorf("Expected default username %s, got %s", DefaultUsername, issuer.GetUsername())
	}
}

func TestStandaloneTokenIssuer_WithScontrolPath(t *testing.T) {
	issuer := NewStandaloneTokenIssuer(types.NamespacedName{}, "")
	customPath := "/usr/local/bin/scontrol"

	result := issuer.WithScontrolPath(customPath)

	if result.GetScontrolPath() != customPath {
		t.Errorf("Expected scontrolPath %s, got %s", customPath, result.GetScontrolPath())
	}
}

func TestStandaloneTokenIssuer_WithRotationInterval(t *testing.T) {
	issuer := NewStandaloneTokenIssuer(types.NamespacedName{}, "")
	customInterval := 1 * time.Hour

	result := issuer.WithRotationInterval(customInterval)

	if result.GetRotationInterval() != customInterval {
		t.Errorf("Expected rotationInterval %v, got %v", customInterval, result.GetRotationInterval())
	}
}

func TestStandaloneTokenIssuer_parseSlurmTokenOutput(t *testing.T) {
	issuer := NewStandaloneTokenIssuer(types.NamespacedName{}, "")

	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{
			name:        "valid SLURM_JWT format",
			input:       "SLURM_JWT=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
			expected:    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
			expectError: false,
		},
		{
			name:        "empty output",
			input:       "",
			expected:    "",
			expectError: true,
		},
		{
			name:        "wrong format",
			input:       "TOKEN=value",
			expected:    "",
			expectError: true,
		},
		{
			name:        "missing value",
			input:       "SLURM_JWT=",
			expected:    "",
			expectError: true,
		},
		{
			name:        "multiple equals",
			input:       "SLURM_JWT=part1=part2",
			expected:    "part1=part2",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := issuer.parseSlurmTokenOutput([]byte(tt.input))

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %s, got %s", tt.expected, result)
				}
			}
		})
	}
}

func TestStandaloneTokenIssuer_Issue_NoToken(t *testing.T) {
	issuer := NewStandaloneTokenIssuer(types.NamespacedName{}, "")
	ctx := context.Background()

	// This will fail because scontrol is not available in test environment
	_, err := issuer.Issue(ctx)
	if err == nil {
		t.Error("Expected error when scontrol is not available")
	}
}
