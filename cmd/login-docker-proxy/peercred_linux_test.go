//go:build linux

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPeerUID(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	result := make(chan struct {
		uid uint32
		err error
	}, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			result <- struct {
				uid uint32
				err error
			}{err: acceptErr}
			return
		}
		defer conn.Close()
		uid, peerErr := peerUID(conn)
		result <- struct {
			uid uint32
			err error
		}{uid: uid, err: peerErr}
	}()

	client, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	peer := <-result
	if peer.err != nil {
		t.Fatalf("peerUID() error = %v", peer.err)
	}
	if want := uint32(os.Getuid()); peer.uid != want {
		t.Fatalf("peer UID = %d, want %d", peer.uid, want)
	}
}
