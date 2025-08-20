package jwt

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type StandaloneTokenIssuer struct {
	clusterID types.NamespacedName
	username  string
	
	currentToken   string
	tokenMu        sync.RWMutex
	rotationTicker *time.Ticker
	stopCh         chan struct{}
	
	scontrolPath      string
	rotationInterval  time.Duration
}

// NewStandaloneTokenIssuer creates a new standalone token issuer
func NewStandaloneTokenIssuer(clusterID types.NamespacedName, username string) *StandaloneTokenIssuer {
	return &StandaloneTokenIssuer{
		clusterID:        clusterID,
		username:         username,
		scontrolPath:     "scontrol", // default path, can be overridden
		rotationInterval: 30 * time.Minute, // rotate every 30 minutes
	}
}

// WithScontrolPath sets a custom path for the scontrol command
func (s *StandaloneTokenIssuer) WithScontrolPath(path string) *StandaloneTokenIssuer {
	s.scontrolPath = path
	return s
}

// WithRotationInterval sets a custom rotation interval
func (s *StandaloneTokenIssuer) WithRotationInterval(interval time.Duration) *StandaloneTokenIssuer {
	s.rotationInterval = interval
	return s
}

// Start begins the token rotation process
func (s *StandaloneTokenIssuer) Start(ctx context.Context) error {
	// Generate initial token
	if err := s.rotateToken(ctx); err != nil {
		return fmt.Errorf("failed to generate initial Slurm token: %w", err)
	}

	// Start rotation ticker
	s.rotationTicker = time.NewTicker(s.rotationInterval)
	s.stopCh = make(chan struct{})

	go s.rotationLoop(ctx)

	return nil
}

// Stop stops the token rotation process
func (s *StandaloneTokenIssuer) Stop() {
	if s.rotationTicker != nil {
		s.rotationTicker.Stop()
	}
	if s.stopCh != nil {
		close(s.stopCh)
	}
}

// rotationLoop runs the token rotation in the background
func (s *StandaloneTokenIssuer) rotationLoop(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("standalone-token-issuer")
	
	for {
		select {
		case <-s.rotationTicker.C:
			if err := s.rotateToken(ctx); err != nil {
				// Log error but continue - we'll use the existing token
				logger.Error(err, "Failed to rotate Slurm token, continuing with existing token")
			} else {
				logger.Info("Successfully rotated Slurm token")
			}
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// rotateToken generates a new Slurm token
func (s *StandaloneTokenIssuer) rotateToken(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("standalone-token-issuer")

	lifespanSeconds := int(s.rotationInterval.Seconds())
	logger.Info("Rotating Slurm token", "rotation_interval", s.rotationInterval, "lifespan_seconds", lifespanSeconds)

	token, err := s.getSlurmToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get new Slurm token: %w", err)
	}

	s.setToken(token)
	logger.Info("Successfully generated new Slurm token", "token_length", len(token))
	return nil
}

// getSlurmToken gets a fresh Slurm token for API calls
func (s *StandaloneTokenIssuer) getSlurmToken(ctx context.Context) (string, error) {
	lifespanSeconds := int(s.rotationInterval.Seconds())

	cmd := exec.CommandContext(ctx, s.scontrolPath, "token", "username=root", fmt.Sprintf("lifespan=%d", lifespanSeconds))
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("scontrol token username=root lifespan=%d failed: %w", lifespanSeconds, err)
	}

	// Parse the output - scontrol token username returns "SLURM_JWT=value"
	outputStr := strings.TrimSpace(string(output))
	if len(outputStr) == 0 {
		return "", fmt.Errorf("scontrol returned empty output")
	}

	// Parse the SLURM_JWT=value format
	parts := strings.Split(outputStr, "=")
	if len(parts) != 2 || parts[0] != "SLURM_JWT" {
		return "", fmt.Errorf("unexpected scontrol output format: %s", outputStr)
	}

	tokenStr := parts[1]
	if len(tokenStr) == 0 {
		return "", fmt.Errorf("scontrol returned empty token")
	}

	return tokenStr, nil
}

// setToken safely sets the current token with proper locking
func (s *StandaloneTokenIssuer) setToken(token string) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	s.currentToken = token
}

// getToken safely retrieves the current token
func (s *StandaloneTokenIssuer) getToken() string {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	return s.currentToken
}

// Issue implements the tokenIssuer interface for standalone mode
func (s *StandaloneTokenIssuer) Issue(ctx context.Context) (string, error) {
	// Get the current cached token
	token := s.getToken()
	if token == "" {
		return "", fmt.Errorf("no Slurm token available")
	}

	return token, nil
}
