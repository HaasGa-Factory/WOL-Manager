//go:build linux

package main

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func secureStateDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if os.Geteuid() == 0 || (ok && stat.Uid == uint32(os.Geteuid())) {
		if err := os.Chmod(path, 0750); err != nil {
			return err
		}
	}
	return chownStatePath(path)
}

func secureStateFile(path string) error {
	if err := os.Chmod(path, 0640); err != nil {
		return err
	}
	return chownStatePath(path)
}

func chownStatePath(path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	account, err := user.Lookup("wol-manager")
	if err != nil {
		return nil
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	return os.Chown(path, uid, gid)
}
