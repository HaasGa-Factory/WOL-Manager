//go:build !linux

package main

import "os"

func secureTokenFile(path string) error {
	return os.Chmod(path, 0600)
}
