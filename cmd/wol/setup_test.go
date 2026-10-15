package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
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
