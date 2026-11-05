//go:build linux

package main

import (
	"os"
	"syscall"
	"unsafe"
)

func terminalWidth(file *os.File) int {
	type windowSize struct {
		row, col, xpixel, ypixel uint16
	}
	var size windowSize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0
	}
	return int(size.col)
}
