package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	if err == errUsage {
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
