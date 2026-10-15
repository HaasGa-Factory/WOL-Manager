//go:build linux

package main

import (
	"os"
	"os/user"
	"strconv"
)

func secureTokenFile(path string) error {
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
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
