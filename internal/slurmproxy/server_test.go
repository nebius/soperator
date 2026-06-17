package slurmproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	internaljwt "nebius.ai/slurm-operator/internal/jwt"
)

type recordingRunner struct {
	name string
	args []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	return []byte("scheduled"), nil
}

func TestServer_RebootNodes(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	runner := &recordingRunner{}
	server, err := NewServer(ServerOptions{
		JWTKey:        key,
		AllowedUsers:  "root",
		ScontrolPath:  "/bin/scontrol",
		CommandRunner: runner,
	})
	require.NoError(t, err)

	body := `{"nodes":["worker-1","worker-1","worker-2"],"reason":"rolling update"}`
	req := httptest.NewRequest(http.MethodPost, EndpointRebootNodes, stringsReader(body))
	req.Header.Set("Authorization", "Bearer "+signedToken(t, key, "root"))
	resp := httptest.NewRecorder()

	server.Handler().ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, "/bin/scontrol", runner.name)
	require.Equal(t, []string{
		"reboot",
		"reason=rolling update",
		"worker-1,worker-2",
	}, runner.args)

	var response RebootNodesResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &response))
	require.Equal(t, []string{"worker-1", "worker-2"}, response.Nodes)
	require.Equal(t, "scheduled", response.Output)
}

func TestServer_RebootNodesRejectsUnauthorizedUser(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	server, err := NewServer(ServerOptions{
		JWTKey:       key,
		AllowedUsers: "root",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, EndpointRebootNodes, stringsReader(`{"nodes":["worker-1"]}`))
	req.Header.Set("Authorization", "Bearer "+signedToken(t, key, "alice"))
	resp := httptest.NewRecorder()

	server.Handler().ServeHTTP(resp, req)

	require.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestRebootNodesRequestValidate(t *testing.T) {
	req := RebootNodesRequest{
		Nodes: []string{"worker-0"},
	}

	require.NoError(t, req.normalizeAndValidate())
	require.Equal(t, DefaultReason, req.Reason)
}

func signedToken(t *testing.T, key []byte, user string) string {
	t.Helper()

	now := time.Now()
	claims := internaljwt.TokenClaims{
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    "soperator",
			Subject:   user,
			IssuedAt:  jwtlib.NewNumericDate(now),
			NotBefore: jwtlib.NewNumericDate(now.Add(-time.Minute)),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(time.Hour)),
		},
		SlurmUsername: user,
	}

	token, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString(key)
	require.NoError(t, err)
	return token
}

func stringsReader(s string) *strings.Reader {
	return strings.NewReader(s)
}
