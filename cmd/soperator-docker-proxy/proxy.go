package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxCreateRequestBody = 16 << 20

var errServerClosed = http.ErrServerClosed

type peerCredentialsContextKey struct{}

type dockerProxy struct {
	resolver            cgroupResolver
	proxy               *httputil.ReverseProxy
	logger              *log.Logger
	logCgroupResolution bool
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			credentials, err := peerCredentialsFor(conn)
			credentials.err = err
			return context.WithValue(ctx, peerCredentialsContextKey{}, credentials)
		},
	}
}

func newDockerProxy(
	resolver cgroupResolver,
	transport http.RoundTripper,
	logger *log.Logger,
	logCgroupResolution bool,
) *dockerProxy {
	upstreamURL := &url.URL{Scheme: "http", Host: "docker"}
	reverseProxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(upstreamURL)
			request.Out.Host = "docker"
			request.Out.Header.Del("Cgroup-Parent")
		},
		Transport:     transport,
		FlushInterval: -1,
		ErrorHandler: func(writer http.ResponseWriter, request *http.Request, err error) {
			logger.Printf("Upstream %s %s failed: %v", request.Method, request.URL.RequestURI(), err)
			http.Error(writer, "Docker daemon is unavailable", http.StatusBadGateway)
		},
	}

	return &dockerProxy{
		resolver:            resolver,
		proxy:               reverseProxy,
		logger:              logger,
		logCgroupResolution: logCgroupResolution,
	}
}

func (proxy *dockerProxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if isContainerCreateRequest(request) {
		credentials, ok := request.Context().Value(peerCredentialsContextKey{}).(peerCredentials)
		if !ok || credentials.err != nil {
			http.Error(writer, "Cannot determine Docker client identity", http.StatusInternalServerError)
			return
		}

		resolution, err := proxy.resolver.Resolve(credentials)
		if err != nil {
			proxy.logger.Printf("Resolve Docker cgroup for pid=%d uid=%d: %v", credentials.pid, credentials.uid, err)
			http.Error(writer, "Cannot determine Docker cgroup policy", http.StatusInternalServerError)
			return
		}
		if proxy.logCgroupResolution {
			proxy.logger.Printf(
				"Resolved pid=%d uid=%d source=%q target=%q",
				credentials.pid,
				credentials.uid,
				resolution.source,
				resolution.parent,
			)
		}

		if err := applyCgroupParent(request, resolution.parent); err != nil {
			var tooLarge *requestBodyTooLargeError
			if errors.As(err, &tooLarge) {
				http.Error(writer, err.Error(), http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
	}

	proxy.proxy.ServeHTTP(writer, request)
}

func isContainerCreateRequest(request *http.Request) bool {
	if request.Method != http.MethodPost {
		return false
	}

	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) == 2 {
		return parts[0] == "containers" && parts[1] == "create"
	}
	if len(parts) != 3 || parts[1] != "containers" || parts[2] != "create" {
		return false
	}
	versionParts := strings.Split(strings.TrimPrefix(parts[0], "v"), ".")
	if !strings.HasPrefix(parts[0], "v") || len(versionParts) != 2 {
		return false
	}
	for _, versionPart := range versionParts {
		if versionPart == "" || strings.IndexFunc(versionPart, func(character rune) bool {
			return character < '0' || character > '9'
		}) != -1 {
			return false
		}
	}
	return true
}

type requestBodyTooLargeError struct {
	limit int64
}

func (err *requestBodyTooLargeError) Error() string {
	return fmt.Sprintf("Docker create request exceeds %d bytes", err.limit)
}

// applyCgroupParent always removes a client-provided value. An empty parent
// selects dockerd's default cgroup placement for non-Slurm worker SSH clients.
func applyCgroupParent(request *http.Request, parent string) error {
	if request.Body == nil {
		return errors.New("Docker create request body is empty")
	}

	body, err := io.ReadAll(io.LimitReader(request.Body, maxCreateRequestBody+1))
	if err != nil {
		return fmt.Errorf("read Docker create request: %w", err)
	}
	if len(body) > maxCreateRequestBody {
		return &requestBodyTooLargeError{limit: maxCreateRequestBody}
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode Docker create request: %w", err)
	}
	if payload == nil {
		return errors.New("Docker create request must be a JSON object")
	}

	hostConfig := make(map[string]json.RawMessage)
	if rawHostConfig, ok := payload["HostConfig"]; ok &&
		!bytes.Equal(bytes.TrimSpace(rawHostConfig), []byte("null")) {
		if err := json.Unmarshal(rawHostConfig, &hostConfig); err != nil || hostConfig == nil {
			return errors.New("Docker create HostConfig must be a JSON object")
		}
	}

	if parent == "" {
		delete(hostConfig, "CgroupParent")
	} else {
		rawCgroupParent, err := json.Marshal(parent)
		if err != nil {
			return fmt.Errorf("encode Docker cgroup parent: %w", err)
		}
		hostConfig["CgroupParent"] = rawCgroupParent
	}
	rawHostConfig, err := json.Marshal(hostConfig)
	if err != nil {
		return fmt.Errorf("encode Docker HostConfig: %w", err)
	}
	payload["HostConfig"] = rawHostConfig

	updatedBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Docker create request: %w", err)
	}
	request.Body = io.NopCloser(bytes.NewReader(updatedBody))
	request.ContentLength = int64(len(updatedBody))
	request.Header.Del("Content-Length")
	request.Header.Del("Cgroup-Parent")
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(updatedBody)), nil
	}
	return nil
}

func unixTransport(socketPath string) *http.Transport {
	return &http.Transport{
		DisableCompression: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
}

func listenUnix(ctx context.Context, socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, fmt.Errorf("create listener directory: %w", err)
	}
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refuse to replace non-socket path %s", socketPath)
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("remove stale listener socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect listener socket: %w", err)
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on Unix socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0o666); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("set listener socket permissions: %w", err)
	}
	return listener, nil
}

func validateSocketPath(name, socketPath string) error {
	if socketPath == "" || !filepath.IsAbs(socketPath) {
		return fmt.Errorf("%s socket path must be absolute", name)
	}
	if filepath.Clean(socketPath) != socketPath || socketPath == string(filepath.Separator) {
		return fmt.Errorf("%s socket path must be a clean file path", name)
	}
	return nil
}
