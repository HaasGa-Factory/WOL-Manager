//go:build linux

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestUpdateInstallerTrust(t *testing.T) {
	path := filepath.Join(testDir(t), "install.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	uid := info.Sys().(*syscall.Stat_t).Uid
	if err := validateInstallerOwner(path, uid); err != nil {
		t.Fatalf("installateur sûr refusé: %v", err)
	}
	if err := os.Chmod(path, 0775); err != nil {
		t.Fatal(err)
	}
	if err := validateInstallerOwner(path, uid); err == nil {
		t.Fatal("installateur modifiable par le groupe accepté")
	}
}
