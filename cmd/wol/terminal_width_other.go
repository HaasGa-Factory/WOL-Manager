//go:build !linux

package main

import "os"

func terminalWidth(_ *os.File) int { return 0 }
