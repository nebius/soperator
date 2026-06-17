package slurmproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	internaljwt "nebius.ai/slurm-operator/internal/jwt"
)

const (
	defaultScontrolPath   = "/usr/bin/scontrol"
	defaultCommandTimeout = 30 * time.Second
	maxReasonLength       = 512
)

var nodeNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecCommandRunner struct{}

func (ExecCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

type ServerOptions struct {
	JWTKey         []byte
	AllowedUsers   string
	ScontrolPath   string
	CommandRunner  CommandRunner
	CommandTimeout time.Duration
}

type Server struct {
	jwtKey         []byte
	allowedUsers   map[string]struct{}
	allowAnyUser   bool
	scontrolPath   string
	commandRunner  CommandRunner
	commandTimeout time.Duration
}

func NewServer(opts ServerOptions) (*Server, error) {
	if len(opts.JWTKey) == 0 {
		return nil, errors.New("jwt key is required")
	}

	allowedUsers, allowAnyUser := parseAllowedUsers(opts.AllowedUsers)
	if len(allowedUsers) == 0 && !allowAnyUser {
		return nil, errors.New("at least one allowed user is required")
	}

	if opts.ScontrolPath == "" {
		opts.ScontrolPath = defaultScontrolPath
	}
	if opts.CommandRunner == nil {
		opts.CommandRunner = ExecCommandRunner{}
	}
	if opts.CommandTimeout == 0 {
		opts.CommandTimeout = defaultCommandTimeout
	}

	return &Server{
		jwtKey:         opts.JWTKey,
		allowedUsers:   allowedUsers,
		allowAnyUser:   allowAnyUser,
		scontrolPath:   opts.ScontrolPath,
		commandRunner:  opts.CommandRunner,
		commandTimeout: opts.CommandTimeout,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(EndpointHealthz, s.handleHealthz)
	mux.HandleFunc(EndpointRebootNodes, s.handleRebootNodes)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRebootNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if _, err := s.authorize(r); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	var req RebootNodesRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	if err := req.normalizeAndValidate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	commandCtx, cancel := context.WithTimeout(r.Context(), s.commandTimeout)
	defer cancel()

	args := []string{"reboot"}
	if req.Reason != "" {
		args = append(args, "reason="+req.Reason)
	}
	args = append(args, strings.Join(req.Nodes, ","))

	output, err := s.commandRunner.Run(commandCtx, s.scontrolPath, args...)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(w, status, strings.TrimSpace(fmt.Sprintf("%v: %s", err, output)))
		return
	}

	writeJSON(w, http.StatusOK, RebootNodesResponse{
		Nodes:  req.Nodes,
		Output: strings.TrimSpace(string(output)),
	})
}

func (s *Server) authorize(r *http.Request) (string, error) {
	raw := r.Header.Get("Authorization")
	if raw == "" {
		return "", errors.New("authorization header is required")
	}
	tokenString, ok := strings.CutPrefix(raw, "Bearer ")
	if !ok || tokenString == "" {
		return "", errors.New("bearer token is required")
	}

	claims := &internaljwt.TokenClaims{}
	token, err := jwtlib.ParseWithClaims(
		tokenString,
		claims,
		func(token *jwtlib.Token) (interface{}, error) {
			return s.jwtKey, nil
		},
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}),
		jwtlib.WithIssuer("soperator"),
	)
	if err != nil {
		return "", fmt.Errorf("invalid token: %w", err)
	}
	if !token.Valid {
		return "", errors.New("invalid token")
	}

	user := claims.SlurmUsername
	if user == "" {
		user = claims.Subject
	}
	if user == "" {
		return "", errors.New("token subject is empty")
	}
	if s.allowAnyUser {
		return user, nil
	}
	if _, ok := s.allowedUsers[user]; !ok {
		return "", fmt.Errorf("user %q is not allowed", user)
	}

	return user, nil
}

func (r *RebootNodesRequest) normalizeAndValidate() error {
	if len(r.Nodes) == 0 {
		return errors.New("nodes must not be empty")
	}

	seen := make(map[string]struct{}, len(r.Nodes))
	nodes := make([]string, 0, len(r.Nodes))
	for _, node := range r.Nodes {
		node = strings.TrimSpace(node)
		if node == "" {
			return errors.New("nodes must not contain empty values")
		}
		if !nodeNamePattern.MatchString(node) {
			return fmt.Errorf("invalid node name %q", node)
		}
		if _, ok := seen[node]; ok {
			continue
		}
		seen[node] = struct{}{}
		nodes = append(nodes, node)
	}
	r.Nodes = nodes

	r.Reason = strings.TrimSpace(r.Reason)
	if r.Reason == "" {
		r.Reason = DefaultReason
	}
	if strings.ContainsAny(r.Reason, "\x00\r\n") {
		return errors.New("reason must not contain control line breaks")
	}
	if len(r.Reason) > maxReasonLength {
		return fmt.Errorf("reason is too long: got %d bytes, max %d", len(r.Reason), maxReasonLength)
	}

	return nil
}

func parseAllowedUsers(raw string) (map[string]struct{}, bool) {
	if raw == "" {
		raw = DefaultAllowedUsers
	}
	res := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		user := strings.TrimSpace(part)
		if user == "" {
			continue
		}
		if user == "*" {
			return nil, true
		}
		res[user] = struct{}{}
	}
	return res, false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}
