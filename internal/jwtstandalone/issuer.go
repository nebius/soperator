package jwtstandalone

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

const (
	DefaultScontrolPath     = "scontrol"
	DefaultRotationInterval = 30 * time.Minute
	DefaultUsername         = "root"
)

// StandaloneTokenIssuer generates Slurm tokens for standalone Slurm clusters
// using scontrol commands instead of Kubernetes secrets
type StandaloneTokenIssuer struct {
	clusterID        types.NamespacedName
	username         string
	currentToken     string
	tokenExpiry      time.Time
	tokenMu          sync.RWMutex
	scontrolPath     string
	rotationInterval time.Duration
}

func NewStandaloneTokenIssuer(clusterID types.NamespacedName, username string) *StandaloneTokenIssuer {
	if username == "" {
		username = DefaultUsername
	}

	return &StandaloneTokenIssuer{
		clusterID:        clusterID,
		username:         username,
		scontrolPath:     DefaultScontrolPath,
		rotationInterval: DefaultRotationInterval,
	}
}

func (s *StandaloneTokenIssuer) WithScontrolPath(path string) *StandaloneTokenIssuer {
	s.scontrolPath = path
	return s
}

func (s *StandaloneTokenIssuer) WithRotationInterval(interval time.Duration) *StandaloneTokenIssuer {
	s.rotationInterval = interval
	return s
}

func (s *StandaloneTokenIssuer) Issue(ctx context.Context) (string, error) {
	s.tokenMu.RLock()

	// Check if we have a valid cached token
	if s.currentToken != "" && time.Now().Before(s.tokenExpiry) {
		token := s.currentToken
		s.tokenMu.RUnlock()
		return token, nil
	}
	s.tokenMu.RUnlock()

	return s.refreshToken(ctx)
}

// refreshToken gets a new token and caches it
func (s *StandaloneTokenIssuer) refreshToken(ctx context.Context) (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	// Double-check after acquiring lock
	if s.currentToken != "" && time.Now().Before(s.tokenExpiry) {
		return s.currentToken, nil
	}

	logger := log.FromContext(ctx).WithName("jwtstandalone")

	// Get new token
	token, err := s.getSlurmToken(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get new Slurm token: %w", err)
	}

	// Calculate expiry time (slightly before rotation interval to be safe)
	expiryTime := time.Now().Add(s.rotationInterval - 30*time.Second)

	// Cache the token
	s.currentToken = token
	s.tokenExpiry = expiryTime

	logger.Info("Generated new Slurm token", "expires_at", expiryTime, "rotation_interval", s.rotationInterval)

	return token, nil
}

func (s *StandaloneTokenIssuer) getSlurmToken(ctx context.Context) (string, error) {
	lifespanSeconds := int(s.rotationInterval.Seconds())

	cmd := exec.CommandContext(ctx, s.scontrolPath, "token", fmt.Sprintf("username=%s", s.username), fmt.Sprintf("lifespan=%d", lifespanSeconds))
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("scontrol token username=%s lifespan=%d failed: %w", s.username, lifespanSeconds, err)
	}

	return s.parseSlurmTokenOutput(output)
}

func (s *StandaloneTokenIssuer) parseSlurmTokenOutput(output []byte) (string, error) {
	outputStr := strings.TrimSpace(string(output))
	if len(outputStr) == 0 {
		return "", fmt.Errorf("scontrol returned empty output")
	}

	if !strings.HasPrefix(outputStr, "SLURM_JWT=") {
		return "", fmt.Errorf("unexpected scontrol output format: %s", outputStr)
	}

	tokenStr := strings.TrimPrefix(outputStr, "SLURM_JWT=")
	if len(tokenStr) == 0 {
		return "", fmt.Errorf("scontrol returned empty token")
	}

	return tokenStr, nil
}

func (s *StandaloneTokenIssuer) GetClusterID() types.NamespacedName {
	return s.clusterID
}

func (s *StandaloneTokenIssuer) GetUsername() string {
	return s.username
}

func (s *StandaloneTokenIssuer) GetScontrolPath() string {
	return s.scontrolPath
}

func (s *StandaloneTokenIssuer) GetRotationInterval() time.Duration {
	return s.rotationInterval
}
