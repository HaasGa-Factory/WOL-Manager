//go:build linux

package main

import (
	"net"
	"syscall"
)

func sendMagic(destination, source string, broadcast bool, packet []byte) error {
	dst, err := net.ResolveUDPAddr("udp4", destination)
	if err != nil {
		return err
	}
	var src *net.UDPAddr
	if source != "" {
		src = &net.UDPAddr{IP: net.ParseIP(source)}
	}
	conn, err := net.ListenUDP("udp4", src)
	if err != nil {
		return err
	}
	defer conn.Close()
	if broadcast {
		raw, err := conn.SyscallConn()
		if err != nil {
			return err
		}
		var sockErr error
		if err = raw.Control(func(fd uintptr) {
			sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
		}); err != nil {
			return err
		}
		if sockErr != nil {
			return sockErr
		}
	}
	_, err = conn.WriteToUDP(packet, dst)
	return err
}
