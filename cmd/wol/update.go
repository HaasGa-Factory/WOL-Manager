//go:build linux

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

const trustedInstaller = "/usr/local/lib/wol-manager/install.sh"

func updateCLI(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fs.Bool("check", false, "vérifier sans installer")
	channel := fs.String("channel", "main", "canal main ou dev")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return errUsage
	}
	if *channel != "main" && *channel != "dev" {
		return errors.New("canal invalide: utilisez main ou dev")
	}
	if err := validateInstaller(trustedInstaller); err != nil {
		return err
	}
	installerArgs := []string{"--update", "--channel", *channel}
	if *check {
		installerArgs = append(installerArgs, "--check")
	}
	program := trustedInstaller
	if !*check && os.Geteuid() != 0 {
		program = "/usr/bin/sudo"
		installerArgs = append([]string{trustedInstaller}, installerArgs...)
	}
	cmd := exec.Command(program, installerArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func validateInstaller(path string) error {
	return validateInstallerOwner(path, 0)
}

func validateInstallerOwner(path string, owner uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("installateur système indisponible: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0022 != 0 {
		return errors.New("installateur système non fiable: fichier régulier non modifiable par groupe/autres requis")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != owner {
		return fmt.Errorf("installateur système non fiable: propriétaire UID %d requis", owner)
	}
	return nil
}
