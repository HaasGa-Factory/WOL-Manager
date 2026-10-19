//go:build !linux

package main

import "os"

func secureStateDir(path string) error {
	return os.Chmod(path, 0750)
}

func secureStateFile(path string) error {
	return os.Chmod(path, 0640)
}
