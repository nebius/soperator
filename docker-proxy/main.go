package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var apiVersionPath = regexp.MustCompile(`^/v[0-9]+(?:\.[0-9]+)?(/.*)?$`)
var errAfterHijack = errors.New("proxy connection became hijacked")

func main() {
	var listenPath string
	var upstreamPath string

	flag.StringVar(&listenPath, "listen", "/var/run/soperator-docker.sock", "Unix socket path to listen on")
	flag.StringVar(&upstreamPath, "upstream", "/var/run/docker.sock", "Unix socket path to proxy to")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(listenPath), 0o755); err != nil {
		log.Fatalf("create listener directory: %v", err)
	}

	if err := removeStaleSocket(listenPath); err != nil {
		log.Fatalf("prepare listener socket: %v", err)
	}

	listener, err := net.Listen("unix", listenPath)
	if err != nil {
		log.Fatalf("listen on %s: %v", listenPath, err)
	}
	defer listener.Close()

	if err := os.Chmod(listenPath, 0o666); err != nil {
		log.Fatalf("chmod %s: %v", listenPath, err)
	}

	server := &http.Server{
		Handler:           newProxyHandler(upstreamPath),
		ReadHeaderTimeout: 30 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-shutdownCtx.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	log.Printf("proxy listening on unix://%s and forwarding to unix://%s", listenPath, upstreamPath)

	err = server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}

func newProxyHandler(upstreamPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if err := applyDefaultCgroupParent(req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		req.Header.Del("Cgroup-Parent")

		if err := proxyRequest(w, req, upstreamPath); err != nil {
			if errors.Is(err, errAfterHijack) {
				return
			}
			log.Printf("proxy error for %s %s: %v", req.Method, req.URL.Path, err)
			http.Error(w, "proxy error", http.StatusBadGateway)
		}
	})
}

func proxyRequest(w http.ResponseWriter, req *http.Request, upstreamPath string) error {
	if isUpgradeRequest(req) {
		return proxyUpgradeRequest(w, req, upstreamPath)
	}

	upstreamConn, err := (&net.Dialer{}).DialContext(req.Context(), "unix", upstreamPath)
	if err != nil {
		return err
	}
	defer upstreamConn.Close()

	return proxyRequestToConn(w, req, upstreamConn)
}

func proxyRequestToConn(w http.ResponseWriter, req *http.Request, upstreamConn net.Conn) error {
	reqCtx := req.Context()

	outReq := req.Clone(req.Context())
	outReq.URL = cloneURLForOriginForm(req.URL)
	outReq.RequestURI = ""
	if outReq.Host == "" {
		outReq.Host = "docker"
	}

	errCh := make(chan error, 1)
	writeErrCh := (<-chan error)(errCh)
	go func() {
		errCh <- outReq.Write(upstreamConn)
	}()

	upstreamReader := bufio.NewReader(upstreamConn)
	resp, err := http.ReadResponse(upstreamReader, outReq)
	if err != nil {
		if err := maybeWaitForRequestWrite(writeErrCh); err != nil {
			return err
		}
		return err
	}
	defer resp.Body.Close()

	if shouldStreamResponse(req, resp) {
		if writeErrCh != nil {
			_ = req.Body.Close()
			if err := maybeWaitForRequestWrite(writeErrCh); err != nil {
				return err
			}
		}
		return streamHijackedConnection(w, req, upstreamConn, upstreamReader, resp)
	}

	if err := waitForRequestWrite(reqCtx, writeErrCh); err != nil {
		return err
	}

	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func proxyUpgradeRequest(w http.ResponseWriter, req *http.Request, upstreamPath string) error {
	upstreamConn, err := (&net.Dialer{}).DialContext(req.Context(), "unix", upstreamPath)
	if err != nil {
		return err
	}
	defer upstreamConn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return errors.New("response writer does not support hijacking")
	}

	clientConn, clientRW, err := hijacker.Hijack()
	if err != nil {
		return err
	}
	defer clientConn.Close()

	outReq := req.Clone(req.Context())
	outReq.URL = cloneURLForOriginForm(req.URL)
	outReq.RequestURI = ""
	if outReq.Host == "" {
		outReq.Host = "docker"
	}
	outReq.Body = nil
	outReq.GetBody = nil
	outReq.ContentLength = 0
	outReq.TransferEncoding = nil

	if err := outReq.Write(upstreamConn); err != nil {
		_, _ = io.WriteString(clientConn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 11\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nproxy error")
		return err
	}
	if req.Body != nil {
		_ = req.Body.Close()
	}

	return tunnelRawHijackedConnection(clientConn, clientRW.Reader, upstreamConn, attachesStdin(req))
}

func waitForRequestWrite(ctx context.Context, errCh <-chan error) error {
	if errCh == nil {
		return nil
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func maybeWaitForRequestWrite(errCh <-chan error) error {
	if errCh == nil {
		return nil
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
			return err
		}
	default:
	}

	return nil
}

func cloneURLForOriginForm(in *url.URL) *url.URL {
	out := *in
	out.Scheme = ""
	out.Host = ""
	return &out
}

func shouldStreamResponse(req *http.Request, resp *http.Response) bool {
	if resp.StatusCode == http.StatusSwitchingProtocols {
		return true
	}

	if strings.Contains(strings.ToLower(resp.Header.Get("Connection")), "upgrade") {
		return true
	}

	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/vnd.docker.raw-stream") {
		return true
	}

	if req.Header.Get("Upgrade") != "" {
		return true
	}

	return false
}

func isUpgradeRequest(req *http.Request) bool {
	if req.Header.Get("Upgrade") != "" {
		return true
	}

	return strings.Contains(strings.ToLower(req.Header.Get("Connection")), "upgrade")
}

func attachesStdin(req *http.Request) bool {
	return req.URL.Query().Get("stdin") == "1"
}

func streamHijackedConnection(w http.ResponseWriter, req *http.Request, upstreamConn net.Conn, upstreamReader *bufio.Reader, resp *http.Response) error {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return errors.New("response writer does not support hijacking")
	}

	clientConn, rw, err := hijacker.Hijack()
	if err != nil {
		return err
	}
	defer clientConn.Close()

	if err := writeResponseHead(rw.Writer, resp); err != nil {
		return err
	}
	if err := rw.Flush(); err != nil {
		return err
	}

	type streamResult struct {
		copyErr error
		dir     string
	}

	errCh := make(chan streamResult, 2)
	pendingResults := 1

	go func() {
		_, copyErr := io.Copy(clientConn, upstreamReader)
		if cw, ok := clientConn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		errCh <- streamResult{copyErr: copyErr, dir: "upstream_to_client"}
	}()

	if attachesStdin(req) {
		pendingResults++
		go func() {
			_, copyErr := io.Copy(upstreamConn, rw.Reader)
			if cw, ok := upstreamConn.(interface{ CloseWrite() error }); ok {
				_ = cw.CloseWrite()
			}
			errCh <- streamResult{copyErr: copyErr, dir: "client_to_upstream"}
		}()
	}

	var firstErr error
	firstResult := <-errCh
	if firstResult.copyErr != nil && !errors.Is(firstResult.copyErr, net.ErrClosed) && !errors.Is(firstResult.copyErr, io.EOF) {
		firstErr = firstResult.copyErr
	}
	if firstResult.dir == "upstream_to_client" {
		_ = clientConn.Close()
		_ = upstreamConn.Close()
	}

	if pendingResults == 2 {
		secondResult := <-errCh
		if secondResult.copyErr != nil && !errors.Is(secondResult.copyErr, net.ErrClosed) && !errors.Is(secondResult.copyErr, io.EOF) && firstErr == nil {
			firstErr = secondResult.copyErr
		}
	}
	_ = clientConn.Close()
	_ = upstreamConn.Close()

	if firstErr != nil {
		return fmt.Errorf("%w: %v", errAfterHijack, firstErr)
	}

	return errAfterHijack
}

func tunnelRawHijackedConnection(clientConn net.Conn, clientReader io.Reader, upstreamConn net.Conn, forwardClientInput bool) error {
	type streamResult struct {
		copyErr error
		dir     string
	}

	pendingResults := 1
	errCh := make(chan streamResult, 2)

	go func() {
		_, copyErr := io.Copy(clientConn, upstreamConn)
		if cw, ok := clientConn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		errCh <- streamResult{copyErr: copyErr, dir: "upstream_to_client"}
	}()

	if forwardClientInput {
		pendingResults++
		go func() {
			_, copyErr := io.Copy(upstreamConn, clientReader)
			if cw, ok := upstreamConn.(interface{ CloseWrite() error }); ok {
				_ = cw.CloseWrite()
			}
			errCh <- streamResult{copyErr: copyErr, dir: "client_to_upstream"}
		}()
	}

	var firstErr error
	firstResult := <-errCh
	if firstResult.copyErr != nil && !errors.Is(firstResult.copyErr, net.ErrClosed) && !errors.Is(firstResult.copyErr, io.EOF) {
		firstErr = firstResult.copyErr
	}
	if firstResult.dir == "upstream_to_client" {
		_ = clientConn.Close()
		_ = upstreamConn.Close()
	}

	if pendingResults == 2 {
		secondResult := <-errCh
		if secondResult.copyErr != nil && !errors.Is(secondResult.copyErr, net.ErrClosed) && !errors.Is(secondResult.copyErr, io.EOF) && firstErr == nil {
			firstErr = secondResult.copyErr
		}
	}
	_ = clientConn.Close()
	_ = upstreamConn.Close()

	if firstErr != nil {
		return fmt.Errorf("%w: %v", errAfterHijack, firstErr)
	}

	return errAfterHijack
}

func writeResponseHead(w *bufio.Writer, resp *http.Response) error {
	if _, err := fmt.Fprintf(w, "HTTP/1.1 %s\r\n", resp.Status); err != nil {
		return err
	}
	if err := resp.Header.Write(w); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\r\n")
	return err
}

func copyHeader(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func applyDefaultCgroupParent(req *http.Request) error {
	headerValue := req.Header.Get("Cgroup-Parent")
	if headerValue == "" || !isContainerCreateRequest(req) {
		return nil
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}
	_ = req.Body.Close()

	if len(body) == 0 {
		body = []byte(`{}`)
	}

	updatedBody, changed, err := setDefaultCgroupParent(body, headerValue)
	if err != nil {
		return err
	}
	if !changed {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
		return nil
	}

	req.Body = io.NopCloser(bytes.NewReader(updatedBody))
	req.ContentLength = int64(len(updatedBody))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(updatedBody)), nil
	}
	req.Header.Set("Content-Length", strconv.Itoa(len(updatedBody)))
	req.TransferEncoding = nil

	return nil
}

func setDefaultCgroupParent(body []byte, cgroupParent string) ([]byte, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, err
	}

	hostConfig, ok := payload["HostConfig"]
	if !ok || hostConfig == nil {
		payload["HostConfig"] = map[string]any{"CgroupParent": cgroupParent}
	} else {
		hostConfigMap, ok := hostConfig.(map[string]any)
		if !ok {
			return nil, false, errors.New("HostConfig must be a JSON object")
		}

		current, exists := hostConfigMap["CgroupParent"]
		if exists {
			if currentString, ok := current.(string); ok && currentString != "" {
				return body, false, nil
			}
			if current != nil && !ok {
				return nil, false, errors.New("HostConfig.CgroupParent must be a string when present")
			}
		}

		hostConfigMap["CgroupParent"] = cgroupParent
	}

	updatedBody, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}

	return updatedBody, true, nil
}

func isContainerCreateRequest(req *http.Request) bool {
	if req.Method != http.MethodPost {
		return false
	}

	path := req.URL.Path
	if matches := apiVersionPath.FindStringSubmatch(path); matches != nil {
		if matches[1] == "" {
			path = "/"
		} else {
			path = matches[1]
		}
	}

	return path == "/containers/create"
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("listener path exists and is not a socket")
	}

	return os.Remove(path)
}
