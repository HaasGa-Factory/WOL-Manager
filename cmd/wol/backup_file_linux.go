//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func secureBackupDir(path string) error {
	if err := os.Chmod(path, 0700); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		return os.Chown(path, 0, 0)
	}
	return nil
}

func secureBackupFile(path string) error {
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		return os.Chown(path, 0, 0)
	}
	return nil
}

func backupArchivePermissionsSafe(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().Perm()&0077 == 0
}

func rejectBackupSymlinkComponents(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	current := string(os.PathSeparator)
	for _, part := range strings.Split(strings.TrimPrefix(abs, string(os.PathSeparator)), string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("chemin refusé: lien symbolique %s", current)
		}
	}
	return nil
}
