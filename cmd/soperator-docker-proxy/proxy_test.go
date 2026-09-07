package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type staticResolver struct {
	resolution cgroupResolution
	err        error
	calls      int
}

func (resolver *staticResolver) Resolve(_ peerCredentials) (cgroupResolution, error) {
	resolver.calls++
	return resolver.resolution, resolver.err
}

func TestIsContainerCreateRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{name: "unversioned", method: http.MethodPost, path: "/containers/create", want: true},
		{name: "versioned", method: http.MethodPost, path: "/v1.55/containers/create", want: true},
		{name: "trailing slash", method: http.MethodPost, path: "/v1.55/containers/create/", want: true},
		{name: "wrong method", method: http.MethodGet, path: "/v1.55/containers/create"},
		{name: "container start", method: http.MethodPost, path: "/v1.55/containers/id/start"},
		{name: "missing version minor", method: http.MethodPost, path: "/v1/containers/create"},
		{name: "invalid version", method: http.MethodPost, path: "/vNaN/containers/create"},
		{name: "extra segment", method: http.MethodPost, path: "/prefix/v1.55/containers/create"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil)
			if got := isContainerCreateRequest(request); got != test.want {
				t.Fatalf("isContainerCreateRequest(%s %q) = %t, want %t", test.method, test.path, got, test.want)
			}
		})
	}
}

func TestApplyCgroupParent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		body             string
		parent           string
		wantParent       string
		wantParentAbsent bool
		wantMemory       string
		wantError        string
	}{
		{
			name:       "missing HostConfig",
			body:       `{"Image":"alpine"}`,
			parent:     "/slurm/job_1/step_0/user",
			wantParent: "/slurm/job_1/step_0/user",
		},
		{
			name:       "null HostConfig",
			body:       `{"HostConfig":null}`,
			parent:     "/base/users/user-1004/docker",
			wantParent: "/base/users/user-1004/docker",
		},
		{
			name:       "preserve fields",
			body:       `{"HostConfig":{"Memory":1024}}`,
			parent:     "/base/users/user-1004/docker",
			wantParent: "/base/users/user-1004/docker",
			wantMemory: "1024",
		},
		{
			name:       "override forged parent",
			body:       `{"HostConfig":{"CgroupParent":"/escape"}}`,
			parent:     "/base/users/user-1004/docker",
			wantParent: "/base/users/user-1004/docker",
		},
		{
			name:             "remove forged parent for direct worker SSH",
			body:             `{"HostConfig":{"CgroupParent":"/another-job","Memory":1024}}`,
			wantParentAbsent: true,
			wantMemory:       "1024",
		},
		{
			name:      "invalid HostConfig",
			body:      `{"HostConfig":[]}`,
			parent:    "/base",
			wantError: "must be a JSON object",
		},
		{
			name:      "invalid body",
			body:      `{`,
			parent:    "/base",
			wantError: "decode Docker create request",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				"/v1.55/containers/create",
				strings.NewReader(test.body),
			)
			request.Header.Set("Cgroup-Parent", "/forged-header")
			err := applyCgroupParent(request, test.parent)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyCgroupParent() error = %v", err)
			}
			if got := request.Header.Get("Cgroup-Parent"); got != "" {
				t.Fatalf("Cgroup-Parent header = %q, want empty", got)
			}

			var payload struct {
				HostConfig map[string]json.RawMessage
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode updated request: %v", err)
			}
			rawParent, exists := payload.HostConfig["CgroupParent"]
			if test.wantParentAbsent {
				if exists {
					t.Fatalf("CgroupParent = %s, want absent", rawParent)
				}
			} else {
				var got string
				if err := json.Unmarshal(rawParent, &got); err != nil {
					t.Fatalf("decode CgroupParent: %v", err)
				}
				if got != test.wantParent {
					t.Fatalf("CgroupParent = %q, want %q", got, test.wantParent)
				}
			}
			if test.wantMemory != "" && string(payload.HostConfig["Memory"]) != test.wantMemory {
				t.Fatalf("HostConfig.Memory = %s, want %s", payload.HostConfig["Memory"], test.wantMemory)
			}
		})
	}
}

func TestApplyCgroupParentRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/containers/create",
		io.LimitReader(zeroReader{}, maxCreateRequestBody+1),
	)
	err := applyCgroupParent(request, "/base")
	var tooLarge *requestBodyTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("error = %v, want requestBodyTooLargeError", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}

func TestProxyForcesResolvedCgroupAndRemovesHeader(t *testing.T) {
	t.Parallel()

	upstreamResult := make(chan struct {
		body   map[string]json.RawMessage
		header string
	}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream body: %v", err)
		}
		upstreamResult <- struct {
			body   map[string]json.RawMessage
			header string
		}{body: body, header: request.Header.Get("Cgroup-Parent")}
		writer.WriteHeader(http.StatusCreated)
	}))
	defer upstream.Close()

	resolver := &staticResolver{resolution: cgroupResolution{parent: "/slurm/job_7/step_0/user"}}
	proxy := newTestProxy(t, upstream.URL, resolver)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"http://proxy/v1.55/containers/create?name=test",
		strings.NewReader(`{"HostConfig":{"CgroupParent":"/escape"}}`),
	)
	request.Header.Set("Cgroup-Parent", "/forged-header")
	request = withPeerCredentials(request, peerCredentials{pid: 42, uid: 1004, gid: 1004})
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.calls)
	}

	result := <-upstreamResult
	if result.header != "" {
		t.Fatalf("upstream Cgroup-Parent header = %q, want empty", result.header)
	}
	var hostConfig map[string]json.RawMessage
	if err := json.Unmarshal(result.body["HostConfig"], &hostConfig); err != nil {
		t.Fatalf("decode upstream HostConfig: %v", err)
	}
	var got string
	if err := json.Unmarshal(hostConfig["CgroupParent"], &got); err != nil {
		t.Fatalf("decode upstream CgroupParent: %v", err)
	}
	if want := "/slurm/job_7/step_0/user"; got != want {
		t.Fatalf("upstream CgroupParent = %q, want %q", got, want)
	}
}

func TestProxySanitizesDirectWorkerSSHRequest(t *testing.T) {
	t.Parallel()

	upstreamResult := make(chan map[string]json.RawMessage, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream body: %v", err)
		}
		upstreamResult <- body
		writer.WriteHeader(http.StatusCreated)
	}))
	defer upstream.Close()

	resolver := &staticResolver{resolution: cgroupResolution{source: "/kubepods/pod/container"}}
	proxy := newTestProxy(t, upstream.URL, resolver)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"http://proxy/containers/create",
		strings.NewReader(`{"HostConfig":{"CgroupParent":"/another-job"}}`),
	)
	request = withPeerCredentials(request, peerCredentials{pid: 42, uid: 1004, gid: 1004})
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}

	result := <-upstreamResult
	var hostConfig map[string]json.RawMessage
	if err := json.Unmarshal(result["HostConfig"], &hostConfig); err != nil {
		t.Fatalf("decode upstream HostConfig: %v", err)
	}
	if parent, ok := hostConfig["CgroupParent"]; ok {
		t.Fatalf("upstream CgroupParent = %s, want absent", parent)
	}
}

func TestProxyRejectsCreateWithoutPeerCredentials(t *testing.T) {
	t.Parallel()

	resolver := &staticResolver{}
	proxy := newTestProxy(t, unreachableUpstream(t), resolver)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/containers/create",
		strings.NewReader(`{}`),
	)
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver calls = %d, want 0", resolver.calls)
	}
}

func TestProxyRejectsResolverFailure(t *testing.T) {
	t.Parallel()

	resolver := &staticResolver{err: errors.New("peer PID is not visible")}
	proxy := newTestProxy(t, unreachableUpstream(t), resolver)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/containers/create",
		strings.NewReader(`{}`),
	)
	request = withPeerCredentials(request, peerCredentials{pid: 42, uid: 1004, gid: 1004})
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestProxyDoesNotResolveNonCreateRequests(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Cgroup-Parent"); got != "" {
			t.Errorf("Cgroup-Parent header = %q, want empty", got)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	resolver := &staticResolver{err: errors.New("must not be called")}
	proxy := newTestProxy(t, upstream.URL, resolver)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://proxy/containers/json", nil)
	request.Header.Set("Cgroup-Parent", "/forged-header")
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver calls = %d, want 0", resolver.calls)
	}
}

func TestProxyForwardsUpgradedStream(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			t.Error("upstream response writer does not support hijacking")
			return
		}
		conn, buffer, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack upstream: %v", err)
			return
		}
		defer conn.Close()
		_, _ = fmt.Fprint(buffer, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\nSTREAM_OK\n")
		_ = buffer.Flush()
	}))
	defer upstream.Close()

	resolver := &staticResolver{}
	proxyHandler := newTestProxy(t, upstream.URL, resolver)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		proxyHandler.ServeHTTP(writer, withPeerCredentials(request, peerCredentials{pid: 42, uid: 1004, gid: 1004}))
	}))
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(t.Context(), "tcp", proxyURL.Host)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = fmt.Fprintf(
		conn,
		"POST /v1.55/exec/test/start HTTP/1.1\r\nHost: proxy\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: 0\r\n\r\n",
	)

	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatalf("read upgrade response: %v", err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusSwitchingProtocols)
	}
	stream, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read upgraded stream: %v", err)
	}
	if stream != "STREAM_OK\n" {
		t.Fatalf("upgraded stream = %q, want %q", stream, "STREAM_OK\n")
	}
}

func TestListenUnixRefusesNonSocket(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "proxy.sock")
	if err := os.WriteFile(socketPath, []byte("do not replace"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	listener, err := listenUnix(t.Context(), socketPath)
	if listener != nil {
		_ = listener.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "refuse to replace non-socket") {
		t.Fatalf("listenUnix() error = %v, want refusal", err)
	}
	contents, readErr := os.ReadFile(socketPath)
	if readErr != nil {
		t.Fatalf("read sentinel: %v", readErr)
	}
	if !bytes.Equal(contents, []byte("do not replace")) {
		t.Fatalf("sentinel was modified: %q", contents)
	}
}

func TestValidateSocketPath(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		path      string
		wantError bool
	}{
		{name: "absolute", path: "/run/docker.sock"},
		{name: "empty", wantError: true},
		{name: "relative", path: "run/docker.sock", wantError: true},
		{name: "unclean", path: "/run/../docker.sock", wantError: true},
		{name: "root", path: "/", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateSocketPath("test", test.path)
			if test.wantError && err == nil {
				t.Fatal("validateSocketPath() error = nil, want error")
			}
			if !test.wantError && err != nil {
				t.Fatalf("validateSocketPath() error = %v", err)
			}
		})
	}
}

func withPeerCredentials(request *http.Request, credentials peerCredentials) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), peerCredentialsContextKey{}, credentials))
}

func newTestProxy(t *testing.T, upstream string, resolver cgroupResolver) *dockerProxy {
	t.Helper()
	return newDockerProxy(
		resolver,
		&rewriteTransport{upstream: upstream},
		log.New(io.Discard, "", 0),
		false,
	)
}

type rewriteTransport struct {
	upstream string
}

func (transport *rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	upstream, err := url.Parse(transport.upstream)
	if err != nil {
		return nil, err
	}
	request.URL.Scheme = upstream.Scheme
	request.URL.Host = upstream.Host
	return http.DefaultTransport.RoundTrip(request)
}

func unreachableUpstream(t *testing.T) string {
	t.Helper()
	return "http://127.0.0.1:1"
}
