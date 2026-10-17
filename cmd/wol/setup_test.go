package main

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestListNUTUPS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer connection.Close()
		buffer := make([]byte, 128)
		_, _ = connection.Read(buffer)
		_, acceptErr = connection.Write([]byte("BEGIN LIST UPS\nUPS main \"Onduleur principal\"\nUPS backup \"Secours\"\nEND LIST UPS\n"))
		done <- acceptErr
	}()
	devices, err := listNUTUPS(context.Background(), UPSConfig{Address: listener.Addr().String(), Timeout: Duration(time.Second)})
	if err != nil {
		if sandboxBlocksNetwork(err) {
			t.Skipf("sockets locaux bloqués: %v", err)
		}
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0].Name != "main" || devices[1].Description != "Secours" {
		t.Fatalf("liste NUT incorrecte: %#v", devices)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestNUTRejectsAnOpenNonNUTPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			defer connection.Close()
			buffer := make([]byte, 128)
			_, _ = connection.Read(buffer)
			_, _ = connection.Write([]byte("HTTP/1.1 200 OK\r\n"))
		}
	}()
	_, err = listNUTUPS(context.Background(), UPSConfig{Address: listener.Addr().String(), Timeout: Duration(time.Second)})
	if err != nil && sandboxBlocksNetwork(err) {
		t.Skipf("sockets locaux bloqués: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "protocole NUT") {
		t.Fatalf("un service non-NUT a été accepté: %v", err)
	}
}

func TestWriteSetupConfigPreservesExistingSettings(t *testing.T) {
	directory := testDir(t)
	path := filepath.Join(directory, "wol.yaml")
	base := strings.Replace(minimalConfig, "port: 9", "port: 7 # conserver", 1)
	if err := os.WriteFile(path, []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	configured := map[string]UPSConfig{
		"local-main":    {Address: "127.0.0.1:3493", Name: "main", Timeout: Duration(3 * time.Second)},
		"remote-main":   {Address: "nut-a.example:3493", Name: "ups-a", TLS: true, Timeout: Duration(3 * time.Second)},
		"remote-backup": {Address: "nut-b.example:3493", Name: "ups-b", TLS: true, Timeout: Duration(3 * time.Second)},
	}
	if err := writeSetupConfig(path, []byte(base), configured, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "port: 7") || !strings.Contains(string(data), "conserver") {
		t.Fatalf("paramètre existant perdu:\n%s", data)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.UPS) != 3 || cfg.UPS["local-main"].Name != "main" || cfg.UPS["remote-backup"].Address != "nut-b.example:3493" || cfg.Global.Port != 7 {
		t.Fatalf("configuration incorrecte: %#v", cfg)
	}
	backups, err := filepath.Glob(path + ".bak-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("sauvegarde absente: %v, %v", backups, err)
	}
}

func TestParseNUTSelection(t *testing.T) {
	indices, err := parseSelection("1,3,1", 3)
	if err != nil || len(indices) != 2 || indices[0] != 0 || indices[1] != 2 {
		t.Fatalf("sélection incorrecte: %v, %v", indices, err)
	}
	if _, err := parseSelection("4", 3); err == nil {
		t.Fatal("sélection hors plage acceptée")
	}
}

func TestGuidedSetupCreatesNetworkMachineAndDryRun(t *testing.T) {
	directory := testDir(t)
	path := filepath.Join(directory, "wol.yaml")
	input := strings.NewReader(strings.Join([]string{
		"2",      // NUT: non
		"1", "1", // réseaux: ajouter
		"lan", "192.168.50.0/24", "", "", "", "", "", "4",
		"2", "1", // machines: ajouter
		"test-pc", "Machine test", "02:00:00:00:50:10", "192.168.50.10", "lan", "", "", "1", "90s", "180s", "5s", "4",
		"5", "y", // valider et enregistrer
		"y", "test-pc", // simulation WoL
	}, "\n") + "\n")
	var output strings.Builder
	if err := setupWithIO(path, input, &output); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Networks["lan"].CIDR != "192.168.50.0/24" || cfg.Servers["test-pc"].Network != "lan" {
		t.Fatalf("configuration guidée incorrecte: %#v", cfg)
	}
	initialDelay, timeout, interval := cfg.effectiveVerification("test-pc")
	if initialDelay != 90*time.Second || timeout != 180*time.Second || interval != 5*time.Second {
		t.Fatalf("délais guidés incorrects: %s, %s, %s", initialDelay, timeout, interval)
	}
	if !strings.Contains(output.String(), "dry_run") || !strings.Contains(output.String(), "Configuration enregistrée") {
		t.Fatalf("résultat guidé incomplet:\n%s", output.String())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("permissions = %o, attendu 0640", info.Mode().Perm())
	}
}

func TestGuidedGlobalVerification(t *testing.T) {
	cfg := testConfig(t)
	input := bufio.NewReader(strings.NewReader("10s\n3m\n5s\n"))
	var output strings.Builder
	if !configureGlobalVerification(input, &output, cfg) {
		t.Fatalf("configuration globale non modifiée: %s", output.String())
	}
	initialDelay, timeout, interval := cfg.effectiveVerification("two")
	if initialDelay != 10*time.Second || timeout != 3*time.Minute || interval != 5*time.Second {
		t.Fatalf("délais globaux incorrects: %s, %s, %s", initialDelay, timeout, interval)
	}
}

func TestWriteGlobalVerificationPreservesOtherGlobalSettings(t *testing.T) {
	directory := testDir(t)
	path := filepath.Join(directory, "wol.yaml")
	base := strings.Replace(minimalConfig, "port: 9", "port: 7 # conserver", 1)
	cfg, err := loadConfigData([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Global.Verification = VerificationConfig{
		InitialDelay: durationPointer(90 * time.Second),
		Timeout:      durationPointer(3 * time.Minute),
		Interval:     durationPointer(5 * time.Second),
	}
	if err := writeManagedConfig(path, []byte(base), cfg, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "port: 7 # conserver") || !strings.Contains(string(data), "initial_delay: 1m30s") {
		t.Fatalf("configuration globale mal préservée:\n%s", data)
	}
}

func TestSetupRejectsSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("création de liens symboliques non garantie sous Windows")
	}
	directory := testDir(t)
	realPath := filepath.Join(directory, "real.yaml")
	linkPath := filepath.Join(directory, "wol.yaml")
	if err := os.WriteFile(realPath, []byte(minimalConfig), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := newConfigSession(linkPath); err == nil || !strings.Contains(err.Error(), "lien symbolique") {
		t.Fatalf("lien symbolique accepté: %v", err)
	}
}

func TestDeleteBlockers(t *testing.T) {
	cfg := &Config{
		Servers: map[string]Server{
			"db":  {Dependencies: []string{"nas"}},
			"nas": {Groups: []string{"infra"}},
		},
		Groups: map[string]Group{"infra": {Servers: []string{"nas"}}},
	}
	if blocker := serverDeleteBlocker(cfg, "nas"); blocker == "" {
		t.Fatal("suppression en cascade de machine non bloquée")
	}
	if blocker := groupDeleteBlocker(cfg, "infra"); blocker == "" {
		t.Fatal("suppression en cascade de groupe non bloquée")
	}
}
