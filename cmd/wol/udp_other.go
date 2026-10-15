//go:build !linux

package main

import "net"

func sendMagic(destination, source string, broadcast bool, packet []byte) error {
	dst, err := net.ResolveUDPAddr("udp4", destination)
	if err != nil {
		return err
	}
	var src *net.UDPAddr
	if source != "" {
		src = &net.UDPAddr{IP: net.ParseIP(source)}
	}
	conn, err := net.DialUDP("udp4", src, dst)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write(packet)
	return err
}
