package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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

func TestIsContainerCreatePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "unversioned", path: "/containers/create", want: true},
		{name: "versioned", path: "/v1.55/containers/create", want: true},
		{name: "trailing slash", path: "/v1.55/containers/create/", want: true},
		{name: "container start", path: "/v1.55/containers/id/start", want: false},
		{name: "missing version minor", path: "/v1/containers/create", want: false},
		{name: "invalid version", path: "/vNaN/containers/create", want: false},
		{name: "extra segment", path: "/prefix/v1.55/containers/create", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isContainerCreatePath(test.path); got != test.want {
				t.Fatalf("isContainerCreatePath(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}

func TestDockerCgroupParent(t *testing.T) {
	t.Parallel()

	const base = "/kubepods.slice/pod.scope/container.scope"
	if got, want := dockerCgroupParent(base, 1004), base+"/users/user-1004"; got != want {
		t.Fatalf("non-root cgroup = %q, want %q", got, want)
	}
	if got, want := dockerCgroupParent(base, 0), base+"/docker-unattributed"; got != want {
		t.Fatalf("root cgroup = %q, want %q", got, want)
	}
}

func TestForceCgroupParent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{name: "missing HostConfig", body: `{"Image":"alpine"}`},
		{name: "null HostConfig", body: `{"HostConfig": null}`},
		{name: "preserves fields", body: `{"HostConfig":{"Memory":1024}}`},
		{name: "overrides escape", body: `{"HostConfig":{"CgroupParent":"/escape"}}`},
		{name: "invalid HostConfig", body: `{"HostConfig":[]}`, wantError: "must be a JSON object"},
		{name: "invalid body", body: `{`, wantError: "decode Docker create request"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodPost, "/v1.55/containers/create", strings.NewReader(test.body))
			err := forceCgroupParent(request, "/base/users/user-1004")
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("forceCgroupParent() error = %v", err)
			}

			var payload struct {
				HostConfig map[string]json.RawMessage
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode updated request: %v", err)
			}
			var got string
			if err := json.Unmarshal(payload.HostConfig["CgroupParent"], &got); err != nil {
				t.Fatalf("decode CgroupParent: %v", err)
			}
			if want := "/base/users/user-1004"; got != want {
				t.Fatalf("CgroupParent = %q, want %q", got, want)
			}
			if test.name == "preserves fields" && string(payload.HostConfig["Memory"]) != "1024" {
				t.Fatalf("HostConfig.Memory was not preserved: %s", payload.HostConfig["Memory"])
			}
		})
	}
}

func TestProxyForcesPeerCgroupAndRemovesHeader(t *testing.T) {
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

	proxy := newTestProxy(t, upstream.URL)
	request := httptest.NewRequest(
		http.MethodPost,
		"http://proxy/v1.55/containers/create?name=test",
		strings.NewReader(`{"HostConfig":{"CgroupParent":"/escape"}}`),
	)
	request.Header.Set("Cgroup-Parent", "/forged-header")
	request = request.WithContext(context.WithValue(
		request.Context(),
		peerCredentialsContextKey{},
		peerCredentials{uid: 1004},
	))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
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
	const want = "/kubepods.slice/pod.scope/container.scope/users/user-1004"
	if got != want {
		t.Fatalf("upstream CgroupParent = %q, want %q", got, want)
	}
}

func TestProxyRejectsCreateWithoutPeerCredentials(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("upstream must not be called")
	}))
	defer upstream.Close()

	proxy := newTestProxy(t, upstream.URL)
	request := httptest.NewRequest(http.MethodPost, "/containers/create", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
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

	proxyHandler := newTestProxy(t, upstream.URL)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request = request.WithContext(context.WithValue(
			request.Context(),
			peerCredentialsContextKey{},
			peerCredentials{uid: 1004},
		))
		proxyHandler.ServeHTTP(writer, request)
	}))
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	conn, err := net.DialTimeout("tcp", proxyURL.Host, 5*time.Second)
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

func TestValidateCgroupBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  string
		valid bool
	}{
		{value: "/kubepods.slice/pod.scope/container.scope\n", want: "/kubepods.slice/pod.scope/container.scope", valid: true},
		{value: "relative/path", valid: false},
		{value: "/", valid: false},
		{value: "/base/../escape", valid: false},
		{value: "", valid: false},
	}

	for _, test := range tests {
		got, err := validateCgroupBase(test.value)
		if test.valid && err != nil {
			t.Fatalf("validateCgroupBase(%q) error = %v", test.value, err)
		}
		if !test.valid && err == nil {
			t.Fatalf("validateCgroupBase(%q) unexpectedly succeeded", test.value)
		}
		if got != test.want {
			t.Fatalf("validateCgroupBase(%q) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestWaitForCgroupBase(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	filePath := filepath.Join(directory, "cgroup-base")
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(filePath, []byte("/kubepods.slice/pod.scope/container.scope\n"), 0o644)
	}()
	got, err := waitForCgroupBase(context.Background(), filePath, 2*time.Second)
	if err != nil {
		t.Fatalf("waitForCgroupBase() error = %v", err)
	}
	if want := "/kubepods.slice/pod.scope/container.scope"; got != want {
		t.Fatalf("cgroup base = %q, want %q", got, want)
	}
}

func TestListenUnixRefusesNonSocket(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "proxy.sock")
	if err := os.WriteFile(socketPath, []byte("do not replace"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	listener, err := listenUnix(socketPath)
	if listener != nil {
		_ = listener.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "refusing to replace non-socket") {
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

func newTestProxy(t *testing.T, upstream string) *dockerProxy {
	t.Helper()
	upstreamURL, err := url.Parse(upstream)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	return newDockerProxy(
		"/kubepods.slice/pod.scope/container.scope",
		upstreamURL,
		http.DefaultTransport,
		log.New(io.Discard, "", 0),
	)
}
