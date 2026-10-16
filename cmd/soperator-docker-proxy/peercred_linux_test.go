//go:build linux

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPeerCredentialsFor(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "peer.sock")
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	result := make(chan struct {
		credentials peerCredentials
		err         error
	}, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			result <- struct {
				credentials peerCredentials
				err         error
			}{err: acceptErr}
			return
		}
		defer conn.Close()
		credentials, peerErr := peerCredentialsFor(conn)
		result <- struct {
			credentials peerCredentials
			err         error
		}{credentials: credentials, err: peerErr}
	}()

	dialer := net.Dialer{Timeout: 5 * time.Second}
	client, err := dialer.DialContext(t.Context(), "unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	peer := <-result
	if peer.err != nil {
		t.Fatalf("peerCredentialsFor() error = %v", peer.err)
	}
	if want := int32(os.Getpid()); peer.credentials.pid != want {
		t.Fatalf("peer PID = %d, want %d", peer.credentials.pid, want)
	}
	if want := uint32(os.Getuid()); peer.credentials.uid != want {
		t.Fatalf("peer UID = %d, want %d", peer.credentials.uid, want)
	}
	if want := uint32(os.Getgid()); peer.credentials.gid != want {
		t.Fatalf("peer GID = %d, want %d", peer.credentials.gid, want)
	}
}
