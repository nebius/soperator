package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestIsContainerCreateRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "plain", path: "/containers/create", want: true},
		{name: "versioned", path: "/v1.50/containers/create", want: true},
		{name: "other", path: "/v1.50/containers/json", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequest(http.MethodPost, "http://docker"+tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			if got := isContainerCreateRequest(req); got != tc.want {
				t.Fatalf("isContainerCreateRequest(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestSetDefaultCgroupParent(t *testing.T) {
	t.Parallel()

	body := []byte(`{"Image":"busybox","HostConfig":{"AutoRemove":true}}`)
	updated, changed, err := setDefaultCgroupParent(body, "custom.slice")
	if err != nil {
		t.Fatalf("setDefaultCgroupParent: %v", err)
	}
	if !changed {
		t.Fatal("expected payload to change")
	}

	var payload map[string]any
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("unmarshal updated body: %v", err)
	}

	hostConfig, ok := payload["HostConfig"].(map[string]any)
	if !ok {
		t.Fatal("HostConfig missing from payload")
	}

	if got := hostConfig["CgroupParent"]; got != "custom.slice" {
		t.Fatalf("CgroupParent = %v, want %q", got, "custom.slice")
	}
}

func TestApplyDefaultCgroupParentPreservesExplicitValue(t *testing.T) {
	t.Parallel()

	body := []byte(`{"Image":"busybox","HostConfig":{"CgroupParent":"already.set"}}`)
	req, err := http.NewRequest(http.MethodPost, "http://docker/v1.50/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Cgroup-Parent", "custom.slice")

	if err := applyDefaultCgroupParent(req); err != nil {
		t.Fatalf("applyDefaultCgroupParent: %v", err)
	}

	updatedBody := make([]byte, req.ContentLength)
	if _, err := req.Body.Read(updatedBody); err != nil {
		t.Fatalf("read body: %v", err)
	}

	if string(updatedBody) != string(body) {
		t.Fatalf("body changed unexpectedly: %s", updatedBody)
	}
}

func TestProxyRequestToConnStreamsResponseBeforeRequestBodyEOF(t *testing.T) {
	t.Parallel()

	upstreamClient, upstreamServer := net.Pipe()
	defer upstreamClient.Close()
	defer upstreamServer.Close()

	clientConn, clientPeer := net.Pipe()
	defer clientPeer.Close()

	body := &blockingReadCloser{
		firstChunk: []byte("stdin-chunk"),
		waitCh:     make(chan struct{}),
	}
	req, err := http.NewRequest(http.MethodPost, "http://docker/containers/create", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	outputCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(clientPeer)
		outputCh <- string(data)
	}()

	go func() {
		reader := bufio.NewReader(upstreamServer)
		if _, err := http.ReadRequest(reader); err != nil {
			return
		}

		if _, err := io.WriteString(upstreamServer, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\nproxy-stream-ok"); err != nil {
			return
		}

		_ = upstreamServer.Close()
	}()

	hijackWriter := &hijackResponseWriter{
		header: http.Header{},
		conn:   clientConn,
		rw:     bufio.NewReadWriter(bufio.NewReader(clientConn), bufio.NewWriter(clientConn)),
	}

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- proxyRequestToConn(hijackWriter, req, upstreamClient)
	}()

	select {
	case err := <-resultCh:
		if err == nil || !errors.Is(err, errAfterHijack) {
			t.Fatalf("proxyRequestToConn() error = %v, want errors.Is(..., %v)", err, errAfterHijack)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proxyRequestToConn timed out waiting for a streamed response")
	}

	body.Close()

	select {
	case output := <-outputCh:
		if !strings.Contains(output, "proxy-stream-ok") {
			t.Fatalf("client output = %q, want streamed payload", output)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for streamed client output")
	}
}

func TestProxyRequestToConnFlushesHeadersBeforeResponseBodyEOF(t *testing.T) {
	t.Parallel()

	upstreamClient, upstreamServer := net.Pipe()
	defer upstreamClient.Close()
	defer upstreamServer.Close()

	req, err := http.NewRequest(http.MethodPost, "http://docker/containers/test/wait?condition=removed", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	writer := &flushRecorderResponseWriter{header: http.Header{}, flushCh: make(chan struct{})}

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- proxyRequestToConn(writer, req, upstreamClient)
	}()

	go func() {
		reader := bufio.NewReader(upstreamServer)
		if _, err := http.ReadRequest(reader); err != nil {
			return
		}

		_, _ = io.WriteString(upstreamServer, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nContent-Type: application/json\r\n\r\n")
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(upstreamServer, "11\r\n{\"StatusCode\":0}\n\r\n0\r\n\r\n")
		_ = upstreamServer.Close()
	}()

	select {
	case <-writer.flushCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("response headers were not flushed before response body completed")
	}

	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("proxyRequestToConn() error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proxyRequestToConn timed out")
	}
}

type blockingReadCloser struct {
	firstChunk []byte
	sentFirst  bool
	waitCh     chan struct{}
}

func (b *blockingReadCloser) Read(p []byte) (int, error) {
	if !b.sentFirst {
		b.sentFirst = true
		n := copy(p, b.firstChunk)
		return n, nil
	}

	<-b.waitCh
	return 0, io.EOF
}

func (b *blockingReadCloser) Close() error {
	select {
	case <-b.waitCh:
	default:
		close(b.waitCh)
	}
	return nil
}

type hijackResponseWriter struct {
	header http.Header
	conn   net.Conn
	rw     *bufio.ReadWriter
}

func (h *hijackResponseWriter) Header() http.Header {
	return h.header
}

func (h *hijackResponseWriter) Write([]byte) (int, error) {
	return 0, nil
}

func (h *hijackResponseWriter) WriteHeader(statusCode int) {}

func (h *hijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.conn, h.rw, nil
}

type flushRecorderResponseWriter struct {
	header     http.Header
	statusCode int
	body       bytes.Buffer
	flushCh    chan struct{}
	flushed    bool
}

func (f *flushRecorderResponseWriter) Header() http.Header {
	return f.header
}

func (f *flushRecorderResponseWriter) Write(p []byte) (int, error) {
	return f.body.Write(p)
}

func (f *flushRecorderResponseWriter) WriteHeader(statusCode int) {
	f.statusCode = statusCode
}

func (f *flushRecorderResponseWriter) Flush() {
	if f.flushed {
		return
	}
	f.flushed = true
	close(f.flushCh)
}
