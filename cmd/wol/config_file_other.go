//go:build !linux

package main

import "os"

func requireSetupWriteAccess(_ string) error {
	return nil
}

func secureConfigFile(actualPath, _ string) error {
	return os.Chmod(actualPath, 0640)
}
