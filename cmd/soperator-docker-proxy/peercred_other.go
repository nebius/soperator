//go:build !linux

package main

import (
	"errors"
	"net"
)

type peerCredentials struct {
	pid int32
	uid uint32
	gid uint32
	err error
}

func peerCredentialsFor(net.Conn) (peerCredentials, error) {
	return peerCredentials{}, errors.New("Unix peer credentials are supported only on Linux")
}
