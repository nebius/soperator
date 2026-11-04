//go:build linux

package main

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

type peerCredentials struct {
	pid int32
	uid uint32
	gid uint32
	err error
}

func peerCredentialsFor(conn net.Conn) (peerCredentials, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return peerCredentials{}, errors.New("accept only Unix connections")
	}
	rawConn, err := unixConn.SyscallConn()
	if err != nil {
		return peerCredentials{}, fmt.Errorf("access Unix connection: %w", err)
	}

	var credentials *syscall.Ucred
	var socketErr error
	if err := rawConn.Control(func(fd uintptr) {
		credentials, socketErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return peerCredentials{}, fmt.Errorf("inspect Unix connection: %w", err)
	}
	if socketErr != nil {
		return peerCredentials{}, fmt.Errorf("read Unix peer credentials: %w", socketErr)
	}
	if credentials == nil {
		return peerCredentials{}, errors.New("Unix peer credentials are unavailable")
	}
	return peerCredentials{
		pid: credentials.Pid,
		uid: credentials.Uid,
		gid: credentials.Gid,
	}, nil
}
