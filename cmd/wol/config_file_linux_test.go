//go:build linux

package main

import (
	"os"
	"strings"
	"testing"
)

func TestSetupWriteAccessRequiresRootForManagedConfig(t *testing.T) {
	err := setupWriteAccessError("/etc/wol-manager/wol.yaml", 1000)
	if err == nil || !strings.Contains(err.Error(), "sudo wol setup") {
		t.Fatalf("commande corrective absente: %v", err)
	}
	if err := setupWriteAccessError("/etc/wol-manager/wol.yaml", 0); err != nil {
		t.Fatalf("root refusé: %v", err)
	}
	if err := setupWriteAccessError("/tmp/wol.yaml", 1000); err != nil {
		t.Fatalf("configuration personnalisée accessible refusée: %v", err)
	}
}

func TestSetupRefusesManagedConfigBeforePrompt(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("ce contrôle nécessite un utilisateur non-root")
	}
	input := strings.NewReader("6\n")
	var output strings.Builder
	err := setupWithIO("/etc/wol-manager/wol.yaml", input, &output)
	if err == nil || !strings.Contains(err.Error(), "sudo wol setup") {
		t.Fatalf("erreur administrative absente: %v", err)
	}
	if input.Len() != 2 || output.Len() != 0 {
		t.Fatalf("l'assistant a commencé avant le contrôle des droits: entrée=%d sortie=%q", input.Len(), output.String())
	}
}
