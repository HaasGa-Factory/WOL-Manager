//go:build !linux

package main

import "errors"

func updateCLI([]string) error {
	return errors.New("la mise à jour intégrée est disponible uniquement sous Linux")
}
