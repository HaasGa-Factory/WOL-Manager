//go:build linux

package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

func secureConfigFile(actualPath, targetPath string) error {
	if err := os.Chmod(actualPath, 0640); err != nil {
		return fmt.Errorf("permissions de configuration: %w", err)
	}
	cleanTarget := filepath.Clean(targetPath)
	if os.Geteuid() != 0 || !(cleanTarget == "/etc/wol-manager/wol.yaml" || strings.HasPrefix(cleanTarget, "/etc/wol-manager/")) {
		return nil
	}
	group, err := user.LookupGroup("wol-manager")
	if err != nil {
		return fmt.Errorf("groupe wol-manager introuvable: %w", err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return fmt.Errorf("GID wol-manager invalide: %w", err)
	}
	if err := os.Chown(actualPath, 0, gid); err != nil {
		return fmt.Errorf("propriétaire de configuration: %w", err)
	}
	return nil
}
