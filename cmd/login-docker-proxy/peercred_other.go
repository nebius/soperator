//go:build !linux

package main

import (
	"errors"
	"net"
)

func peerUID(_ net.Conn) (uint32, error) {
	return 0, errors.New("Unix peer credentials are only supported on Linux")
}
