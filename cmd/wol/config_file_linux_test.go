//go:build linux

package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestSetupWriteAccessRequiresRootForManagedConfig(t *testing.T) {
	err := setupWriteAccessError("/etc/wol-manager/wol.yaml", 1000)
	if err == nil || !strings.Contains(err.Error(), "sudo wol menu") || !strings.Contains(err.Error(), "sudo wol setup") {
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
	if err == nil || !strings.Contains(err.Error(), "sudo wol menu") || !strings.Contains(err.Error(), "sudo wol setup") {
		t.Fatalf("erreur administrative absente: %v", err)
	}
	if input.Len() != 2 || output.Len() != 0 {
		t.Fatalf("l'assistant a commencé avant le contrôle des droits: entrée=%d sortie=%q", input.Len(), output.String())
	}
}

func TestRedirectedMenuStopsAfterSetupPermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("ce contrôle nécessite un utilisateur non-root")
	}
	inputReader, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldInput, oldOutput := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inputReader, outputWriter
	t.Cleanup(func() {
		os.Stdin, os.Stdout = oldInput, oldOutput
		_ = inputReader.Close()
		_ = outputReader.Close()
	})
	if _, err := inputWriter.WriteString("6\n"); err != nil {
		t.Fatal(err)
	}
	_ = inputWriter.Close()
	if err := menu("/etc/wol-manager/wol.yaml", testConfig(t), &Engine{audit: &Auditor{}}, false); err != nil {
		t.Fatal(err)
	}
	_ = outputWriter.Close()
	text, err := io.ReadAll(outputReader)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(text), "MENU PRINCIPAL"); count != 1 {
		t.Fatalf("le menu redirigé a été réaffiché %d fois:\n%s", count, text)
	}
}
