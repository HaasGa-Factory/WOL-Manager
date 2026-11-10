//go:build !linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func secureBackupDir(path string) error             { return os.Chmod(path, 0700) }
func secureBackupFile(path string) error            { return os.Chmod(path, 0600) }
func backupArchivePermissionsSafe(path string) bool { return true }

func rejectBackupSymlinkComponents(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := abs; ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("chemin refusé: lien symbolique %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}
