package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStatusCLIProcess(t *testing.T) {
	if os.Getenv("WOL_STATUS_HELPER") != "1" {
		return
	}
	separator := 0
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i + 1
			break
		}
	}
	err := run(os.Args[separator:])
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "ERREUR: %v\n", err)
	if errors.Is(err, errUsage) {
		os.Exit(2)
	}
	os.Exit(1)
}

func runStatusProcess(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	commandArgs := append([]string{"-test.run=^TestStatusCLIProcess$", "--"}, args...)
	cmd := exec.Command(os.Args[0], commandArgs...)
	cmd.Env = append(os.Environ(), "WOL_STATUS_HELPER=1", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	}
	t.Fatalf("exécution CLI: %v", err)
	return "", "", -1
}

func writeStatusConfig(t *testing.T, port int) string {
	t.Helper()
	cfg := testConfig(t)
	first := cfg.Servers["one"]
	first.IP = "127.0.0.2"
	cfg.Servers["one"] = first
	server := cfg.Servers["two"]
	server.IP = "127.0.0.1"
	server.Checks = []Check{{Type: "tcp", Port: port}}
	cfg.Servers["two"] = server
	content, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(testDir(t), "wol.yaml")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStatusCLIExitCodesAndStreams(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("socket local indisponible: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	config := writeStatusConfig(t, port)

	stdout, stderr, code := runStatusProcess(t, "--config", config, "status", "two")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"state": "reachable"`) {
		t.Fatalf("machine joignable: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	_ = listener.Close()
	stdout, stderr, code = runStatusProcess(t, "--config", config, "status", "two")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"state": "unreachable"`) {
		t.Fatalf("machine injoignable: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	for _, jsonFlag := range [][]string{nil, {"--json"}} {
		args := append([]string{"--config", config}, jsonFlag...)
		args = append(args, "status", "does-not-exist")
		stdout, stderr, code = runStatusProcess(t, args...)
		if code != 1 || !strings.Contains(stderr, "serveur inconnu") {
			t.Fatalf("machine inconnue: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var result StatusResult
		if err := json.Unmarshal([]byte(stdout), &result); err != nil || result.State != "check_impossible" || result.Note != "serveur inconnu" {
			t.Fatalf("JSON inconnu invalide: %v %#v (%q)", err, result, stdout)
		}
	}
	_, _, code = runStatusProcess(t, "--config", config, "status")
	if code != 2 {
		t.Fatalf("erreur d'utilisation: code=%d, attendu 2", code)
	}
}

func TestUnknownServerAPIStillReturns404(t *testing.T) {
	cfg := testConfig(t)
	store, _ := openJobs(cfg.Storage.StateFile)
	engine := &Engine{cfg: cfg, audit: &Auditor{}}
	srv := &APIServer{cfg: cfg, jobs: store, engine: engine, rates: map[string]rateEntry{}}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/servers/does-not-exist/status", nil)
	r = r.WithContext(context.WithValue(r.Context(), principalKey, principal{ID: "test", permissions: map[string]bool{"read": true}}))
	w := httptest.NewRecorder()
	srv.server(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("API machine inconnue: %d %s", w.Code, w.Body.String())
	}
}

func TestWakeSelectorsAreStrictlyMutuallyExclusive(t *testing.T) {
	config := writeStatusConfig(t, 9)
	cases := [][]string{
		{"wake", "one", "--group", "core", "--dry-run"},
		{"wake", "one", "--all", "--dry-run"},
		{"wake", "--group", "core", "--all", "--dry-run"},
		{"wake", "one", "two", "--dry-run"},
	}
	for _, args := range cases {
		full := append([]string{"--config", config}, args...)
		stdout, stderr, code := runStatusProcess(t, full...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "mutuellement exclusifs") {
			t.Fatalf("sélecteurs %v: code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
}

func TestNetworkTestReturnsFailureForUnknownOrPartialSelection(t *testing.T) {
	config := writeStatusConfig(t, 9)
	for _, ids := range [][]string{{"does-not-exist"}, {"one", "does-not-exist"}} {
		args := []string{"--config", config, "--json", "network", "test"}
		args = append(args, ids...)
		stdout, stderr, code := runStatusProcess(t, args...)
		if code != 1 || !strings.Contains(stderr, "plans réseau") {
			t.Fatalf("sélection %v: code=%d stdout=%q stderr=%q", ids, code, stdout, stderr)
		}
		var result map[string]struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("JSON invalide: %v (%q)", err, stdout)
		}
		if result["does-not-exist"].Error == "" {
			t.Fatalf("erreur inconnue absente: %#v", result)
		}
	}
}

func TestRelayErrorsExplainTheNextStep(t *testing.T) {
	config := writeStatusConfig(t, 9)
	_, stderr, code := runStatusProcess(t, "--config", config, "relay", "serve")
	if code != 2 || !strings.Contains(stderr, "wol relay serve --id") || !strings.Contains(stderr, "--help") {
		t.Fatalf("usage relais incomplète: code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = runStatusProcess(t, "--config", config, "relay", "check", "missing")
	if code != 1 || !strings.Contains(stderr, "déclarez-le") || !strings.Contains(stderr, "wol relay --help") {
		t.Fatalf("relais inconnu sans aide: code=%d stderr=%q", code, stderr)
	}
}

func TestConfigEditReportsNoModification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("éditeur exécutable testé sous Linux")
	}
	config := writeStatusConfig(t, 9)
	editor := filepath.Join(testDir(t), "no-change-editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	oldEditor := os.Getenv("EDITOR")
	if err := os.Setenv("EDITOR", editor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("EDITOR", oldEditor) })
	stdout, stderr, code := runStatusProcess(t, "--config", config, "config", "edit")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Aucune modification enregistrée") {
		t.Fatalf("édition inchangée: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}
