//go:build linux

package main

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

func peerUID(conn net.Conn) (uint32, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("Docker proxy accepted a non-Unix connection")
	}
	rawConn, err := unixConn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("access Unix connection: %w", err)
	}

	var credentials *syscall.Ucred
	var socketErr error
	if err := rawConn.Control(func(fd uintptr) {
		credentials, socketErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, fmt.Errorf("inspect Unix connection: %w", err)
	}
	if socketErr != nil {
		return 0, fmt.Errorf("read Unix peer credentials: %w", socketErr)
	}
	if credentials == nil {
		return 0, errors.New("Unix peer credentials are unavailable")
	}
	return credentials.Uid, nil
}
