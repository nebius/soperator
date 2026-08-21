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
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	defaultProxySocket       = "/run/soperator-docker.sock"
	defaultDaemonSocket      = "/run/docker.sock"
	defaultCgroupBaseFile    = "/run/soperator-docker-cgroup-base"
	defaultCgroupWaitTimeout = 2 * time.Minute
	maxCreateRequestBody     = 16 << 20
)

type config struct {
	proxySocket       string
	daemonSocket      string
	cgroupBaseFile    string
	cgroupWaitTimeout time.Duration
}

type peerCredentials struct {
	uid uint32
	err error
}

type peerCredentialsContextKey struct{}

type dockerProxy struct {
	cgroupBase string
	proxy      *httputil.ReverseProxy
}

func main() {
	logger := log.New(os.Stdout, "Login Docker proxy: ", log.LstdFlags|log.Lmicroseconds)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, defaultConfig(), logger); err != nil {
		logger.Fatalf("%v", err)
	}
}

func defaultConfig() config {
	return config{
		proxySocket:       defaultProxySocket,
		daemonSocket:      defaultDaemonSocket,
		cgroupBaseFile:    defaultCgroupBaseFile,
		cgroupWaitTimeout: defaultCgroupWaitTimeout,
	}
}

func run(ctx context.Context, cfg config, logger *log.Logger) error {
	cgroupBase, err := waitForCgroupBase(ctx, cfg.cgroupBaseFile, cfg.cgroupWaitTimeout)
	if err != nil {
		return fmt.Errorf("resolve cgroup base: %w", err)
	}

	listener, err := listenUnix(cfg.proxySocket)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.proxySocket, err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(cfg.proxySocket)
	}()

	upstreamURL := &url.URL{Scheme: "http", Host: "docker"}
	handler := newDockerProxy(cgroupBase, upstreamURL, unixTransport(cfg.daemonSocket), logger)
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			uid, peerErr := peerUID(conn)
			return context.WithValue(ctx, peerCredentialsContextKey{}, peerCredentials{
				uid: uid,
				err: peerErr,
			})
		},
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	logger.Printf("ready on %s with cgroup base %s", cfg.proxySocket, cgroupBase)
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve proxy: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down proxy: %w", err)
		}
		return nil
	}
}

func newDockerProxy(
	cgroupBase string,
	upstreamURL *url.URL,
	transport http.RoundTripper,
	logger *log.Logger,
) *dockerProxy {
	reverseProxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(upstreamURL)
			request.Out.Host = "docker"
			request.Out.Header.Del("Cgroup-Parent")
		},
		Transport:     transport,
		FlushInterval: -1,
		ErrorHandler: func(writer http.ResponseWriter, request *http.Request, err error) {
			logger.Printf("upstream %s %s failed: %v", request.Method, request.URL.RequestURI(), err)
			http.Error(writer, "Docker daemon is unavailable", http.StatusBadGateway)
		},
	}

	return &dockerProxy{
		cgroupBase: strings.TrimSuffix(cgroupBase, "/"),
		proxy:      reverseProxy,
	}
}

func (proxy *dockerProxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if isContainerCreatePath(request.URL.Path) {
		credentials, ok := request.Context().Value(peerCredentialsContextKey{}).(peerCredentials)
		if !ok || credentials.err != nil {
			http.Error(writer, "Cannot determine Docker client identity", http.StatusInternalServerError)
			return
		}

		cgroupParent := dockerCgroupParent(proxy.cgroupBase, credentials.uid)
		if err := forceCgroupParent(request, cgroupParent); err != nil {
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

func isContainerCreatePath(requestPath string) bool {
	parts := strings.Split(strings.Trim(requestPath, "/"), "/")
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

func dockerCgroupParent(cgroupBase string, uid uint32) string {
	if uid == 0 {
		return cgroupBase + "/docker-unattributed"
	}
	return fmt.Sprintf("%s/users/user-%d", cgroupBase, uid)
}

type requestBodyTooLargeError struct {
	limit int64
}

func (err *requestBodyTooLargeError) Error() string {
	return fmt.Sprintf("Docker create request exceeds %d bytes", err.limit)
}

func forceCgroupParent(request *http.Request, cgroupParent string) error {
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

	rawCgroupParent, err := json.Marshal(cgroupParent)
	if err != nil {
		return fmt.Errorf("encode Docker cgroup parent: %w", err)
	}
	hostConfig["CgroupParent"] = rawCgroupParent
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

func waitForCgroupBase(ctx context.Context, filePath string, timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		contents, err := os.ReadFile(filePath)
		if err == nil {
			cgroupBase, validateErr := validateCgroupBase(string(contents))
			if validateErr == nil {
				return cgroupBase, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
			return "", fmt.Errorf("timed out waiting for %s", filePath)
		case <-ticker.C:
		}
	}
}

func validateCgroupBase(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") {
		return "", errors.New("cgroup base must be an absolute cgroup path")
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "/" {
		return "", errors.New("cgroup base must be a clean non-root cgroup path")
	}
	return strings.TrimSuffix(cleaned, "/"), nil
}

func listenUnix(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to replace non-socket path %s", socketPath)
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o666); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}
