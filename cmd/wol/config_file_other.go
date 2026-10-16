//go:build !linux

package main

import "os"

func secureConfigFile(actualPath, _ string) error {
	return os.Chmod(actualPath, 0640)
}
