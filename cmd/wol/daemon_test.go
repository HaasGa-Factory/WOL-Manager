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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDaemonHelpDescribesIndependentNUTMonitor(t *testing.T) {
	var output bytes.Buffer
	if err := printCommandHelp(&output, []string{"daemon", "--help"}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"wol [--config FILE] daemon", "sans démarrer ni exposer l’API REST", "SIGTERM"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("aide daemon incomplète (%s):\n%s", expected, output.String())
		}
	}
}

func TestSystemdUnitsSeparateDaemonAndAPI(t *testing.T) {
	daemonUnit, err := os.ReadFile(filepath.Join("..", "..", "packaging", "systemd", "wol-manager.service"))
	if err != nil {
		t.Fatal(err)
	}
	apiUnit, err := os.ReadFile(filepath.Join("..", "..", "packaging", "systemd", "wol-manager-api.service"))
	if err != nil {
		t.Fatal(err)
	}
	daemonText, apiText := string(daemonUnit), string(apiUnit)
	for _, expected := range []string{
		"ExecStart=/usr/local/bin/wol --config /etc/wol-manager/wol.yaml daemon",
		"RuntimeDirectory=wol-manager",
		"Restart=on-failure",
		"StandardOutput=journal",
		"StandardError=journal",
		"SyslogLevelPrefix=true",
	} {
		if !strings.Contains(daemonText, expected) {
			t.Fatalf("unité daemon incomplète (%s):\n%s", expected, daemonText)
		}
	}
	if strings.Contains(daemonText, "api serve") || strings.Contains(daemonText, "8080") {
		t.Fatalf("l'unité daemon démarre encore l'API:\n%s", daemonText)
	}
	for _, expected := range []string{"ExecStart=/usr/local/bin/wol --config /etc/wol-manager/wol.yaml api serve", "After=network-online.target wol-manager.service", "Restart=on-failure"} {
		if !strings.Contains(apiText, expected) {
			t.Fatalf("unité API incomplète (%s):\n%s", expected, apiText)
		}
	}
	if strings.Contains(apiText, "Requires=wol-manager.service") {
		t.Fatalf("l'unité API dépend fortement du daemon:\n%s", apiText)
	}
}

func TestLegacyCombinedAPIUsesWritableTemporarySocket(t *testing.T) {
	legacyUnit := filepath.Join(testDir(t), "wol-manager.service")
	if err := os.WriteFile(legacyUnit, []byte("ExecStart=/usr/local/bin/wol --config /etc/wol-manager/wol.yaml api serve\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WOL_SYSTEMD_UNIT", legacyUnit)
	t.Setenv("WOL_DAEMON_SOCKET", "")
	t.Setenv("TMPDIR", testDir(t))

	cfg := testConfig(t)
	cfg.API.Listen = reserveTCPAddress(t)
	cfg.Storage.StateFile = filepath.Join(testDir(t), "state.json")
	cfg.API.TokensFile = filepath.Join(testDir(t), "tokens.json")
	if err := os.WriteFile(cfg.API.TokensFile, []byte(`{"tokens":[{"id":"test","sha256":"0000000000000000000000000000000000000000000000000000000000000000","permissions":["read"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	engine := &Engine{cfg: cfg, audit: &Auditor{}, sendPacket: func(string, string, bool, []byte) error { return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveAPIProcess(ctx, cfg, engine) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", cfg.API.Listen, 50*time.Millisecond)
		if err == nil {
			connection.Close()
			break
		}
		select {
		case processErr := <-done:
			t.Fatalf("mode historique arrêté avant l'API: %v", processErr)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("API historique inaccessible: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("arrêt du mode historique: %v", err)
	}
}

func TestDaemonRunsWithoutOpeningAPI(t *testing.T) {
	var processLog bytes.Buffer

	cfg := testConfig(t)
	cfg.API.Listen = reserveTCPAddress(t)
	group := cfg.Groups["core"]
	group.PowerRestore = &PowerRestorePolicy{Enabled: true, UPS: "ups-main"}
	cfg.Groups["core"] = group
	cfg.UPS["ups-main"] = UPSConfig{Address: "127.0.0.1:3493", Name: "main"}

	store, err := openJobs(filepath.Join(testDir(t), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{cfg: cfg, audit: &Auditor{}, eventLog: func(level, format string, args ...any) {
		fmt.Fprintf(&processLog, "%s %s\n", level, fmt.Sprintf(format, args...))
	}, sendPacket: func(string, string, bool, []byte) error { return nil }}
	socket := filepath.Join(testDir(t), "daemon.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveDaemon(ctx, cfg, store, engine, socket) }()

	client := daemonClient{socket: socket}
	var status daemonStatus
	deadline := time.Now().Add(2 * time.Second)
	for {
		status, err = client.status()
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("daemon inaccessible: %v", err)
	}
	if info, err := os.Stat(socket); err != nil || info.Mode().Perm() != 0600 {
		cancel()
		t.Fatalf("permissions du socket daemon: info=%v err=%v", info, err)
	}
	if !status.Active || !status.Monitoring || status.Policies != 1 {
		cancel()
		t.Fatalf("statut daemon inattendu: %#v", status)
	}
	if connection, dialErr := net.DialTimeout("tcp", cfg.API.Listen, 100*time.Millisecond); dialErr == nil {
		_ = connection.Close()
		cancel()
		t.Fatalf("le daemon a ouvert le port HTTP %s", cfg.API.Listen)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("arrêt daemon: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("le daemon ne s'arrête pas avec son contexte")
	}
	for _, expected := range []string{
		"INFO daemon: démarrage",
		"INFO daemon: moniteur NUT actif",
		"INFO daemon: arrêt demandé",
	} {
		if !strings.Contains(processLog.String(), expected) {
			t.Fatalf("événement daemon absent (%s):\n%s", expected, processLog.String())
		}
	}
}

func TestLegacyCombinedAPIUnitKeepsAutomationUntilServiceMigration(t *testing.T) {
	cfg := testConfig(t)
	cfg.API.Listen = reserveTCPAddress(t)
	cfg.API.TokensFile = filepath.Join(testDir(t), "tokens.json")
	if err := writeTokenFile(cfg.API.TokensFile, tokenFile{}); err != nil {
		t.Fatal(err)
	}
	group := cfg.Groups["core"]
	group.PowerRestore = &PowerRestorePolicy{Enabled: true, UPS: "ups-main"}
	cfg.Groups["core"] = group
	cfg.UPS["ups-main"] = UPSConfig{Address: "127.0.0.1:1", Name: "main"}

	legacyUnit := filepath.Join(testDir(t), "wol-manager.service")
	if err := os.WriteFile(legacyUnit, []byte("ExecStart=/usr/local/bin/wol --config /etc/wol-manager/wol.yaml api serve\n"), 0600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(testDir(t), "daemon.sock")
	t.Setenv("WOL_SYSTEMD_UNIT", legacyUnit)
	t.Setenv("WOL_DAEMON_SOCKET", socket)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveAPIProcess(ctx, cfg, &Engine{cfg: cfg, audit: &Auditor{}, sendPacket: func(string, string, bool, []byte) error { return nil }})
	}()

	client := daemonClient{socket: socket}
	status := waitForDaemon(t, client)
	if !status.Monitoring || status.Policies != 1 {
		t.Fatalf("le mode combiné historique n'a pas préservé la surveillance: %+v", status)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("le service combiné historique ne s'arrête pas")
	}
}

func TestAPIDelegatesWakeJobsToDaemonAndRefusesWhenStopped(t *testing.T) {
	cfg := testConfig(t)
	store, err := openJobs(filepath.Join(testDir(t), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{cfg: cfg, audit: &Auditor{}, sendPacket: func(string, string, bool, []byte) error { return nil }}
	socket := filepath.Join(testDir(t), "daemon.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveDaemon(ctx, cfg, store, engine, socket) }()
	client := &daemonClient{socket: socket}
	waitForDaemon(t, *client)

	server := &APIServer{cfg: cfg, engine: engine, daemon: client, rates: map[string]rateEntry{}}
	wake := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/two/wake", bytes.NewBufferString(`{"dry_run":true}`))
		request = request.WithContext(context.WithValue(request.Context(), principalKey, principal{ID: "api-test", permissions: map[string]bool{"wake": true}}))
		response := httptest.NewRecorder()
		server.server(response, request)
		return response
	}

	response := wake()
	if response.Code != http.StatusAccepted {
		cancel()
		t.Fatalf("réveil délégué: HTTP %d: %s", response.Code, response.Body.String())
	}
	var accepted Job
	if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil || accepted.ID == "" {
		cancel()
		t.Fatalf("réponse job invalide: %v, %#v", err, accepted)
	}
	if persisted, ok := store.get(accepted.ID); !ok || persisted.Actor != "api-test" {
		cancel()
		t.Fatalf("job non créé par le daemon: %#v, présent=%v", persisted, ok)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	response = wake()
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "daemon_unavailable") {
		t.Fatalf("daemon arrêté: HTTP %d: %s", response.Code, response.Body.String())
	}
}

func TestAutomationStatusDistinguishesConfiguredPolicyFromActiveDaemon(t *testing.T) {
	cfg := testConfig(t)
	group := cfg.Groups["core"]
	low, high := 0, 70
	group.PowerRestore = &PowerRestorePolicy{Enabled: true, UPS: "ups-main", LowBattery: &low, HighBattery: &high}
	cfg.Groups["core"] = group
	cfg.UPS["ups-main"] = UPSConfig{Address: "127.0.0.1:3493", Name: "main"}
	store, err := openJobs(filepath.Join(testDir(t), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{cfg: cfg, audit: &Auditor{}, sendPacket: func(string, string, bool, []byte) error { return nil }}
	socket := filepath.Join(testDir(t), "daemon.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveDaemon(ctx, cfg, store, engine, socket) }()
	client := &daemonClient{socket: socket}
	waitForDaemon(t, *client)
	server := &APIServer{cfg: cfg, daemon: client}

	readStatus := func() AutomationRuntimeStatus {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/automations", nil)
		request = request.WithContext(context.WithValue(request.Context(), principalKey, principal{permissions: map[string]bool{"read": true}}))
		response := httptest.NewRecorder()
		server.automationState(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
		}
		var status AutomationRuntimeStatus
		if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		return status
	}

	active := readStatus()
	if !active.Daemon.Active || !active.Daemon.Monitoring || len(active.Policies) != 1 {
		cancel()
		t.Fatalf("statut actif incorrect: %#v", active)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	inactive := readStatus()
	if inactive.Daemon.Active || inactive.Daemon.Monitoring || len(inactive.Policies) != 1 || !strings.Contains(inactive.Message, "aucun daemon actif") {
		t.Fatalf("statut inactif incorrect: %#v", inactive)
	}
}

func TestAutomationStatusCLIReportsInactiveDaemon(t *testing.T) {
	cfg := testConfig(t)
	group := cfg.Groups["core"]
	low, high := 0, 70
	group.PowerRestore = &PowerRestorePolicy{Enabled: true, UPS: "ups-main", LowBattery: &low, HighBattery: &high}
	cfg.Groups["core"] = group
	cfg.UPS["ups-main"] = UPSConfig{Address: "127.0.0.1:3493", Name: "main"}
	content, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(testDir(t), "wol.yaml")
	if err := os.WriteFile(configPath, content, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WOL_DAEMON_SOCKET", filepath.Join(testDir(t), "absent.sock"))
	stdout, stderr, code := runStatusProcess(t, "--config", configPath, "automation", "status")
	if code != 0 || stderr != "" {
		t.Fatalf("automation status: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var status AutomationRuntimeStatus
	stdout = strings.TrimSuffix(stdout, "PASS\n")
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("JSON invalide: %v (%s)", err, stdout)
	}
	if status.Daemon.Active || len(status.Policies) != 1 || !strings.Contains(status.Message, "aucun daemon actif") {
		t.Fatalf("diagnostic inactif incomplet: %#v", status)
	}
}

func TestAutomationStatusDoesNotMisreportSocketPermissionAsInactiveDaemon(t *testing.T) {
	cfg := testConfig(t)
	status := offlineAutomationStatus(cfg, os.ErrPermission)
	if !strings.Contains(status.Message, "sudo wol automation status") {
		t.Fatalf("diagnostic permission ambigu: %q", status.Message)
	}
}

func TestDaemonIsSingleOwnerAndPersistsAPIJobs(t *testing.T) {
	cfg := testConfig(t)
	statePath := filepath.Join(testDir(t), "state.json")
	store, err := openJobs(statePath)
	if err != nil {
		t.Fatal(err)
	}
	packets := 0
	engine := &Engine{cfg: cfg, audit: &Auditor{}, sendPacket: func(string, string, bool, []byte) error {
		packets++
		return nil
	}}
	socket := filepath.Join(testDir(t), "daemon.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveDaemon(ctx, cfg, store, engine, socket) }()
	client := daemonClient{socket: socket}
	waitForDaemon(t, client)

	secondStore, err := openJobs(statePath)
	if err != nil {
		t.Fatal(err)
	}
	secondErr := serveDaemon(context.Background(), cfg, secondStore, engine, socket)
	if secondErr == nil || !strings.Contains(secondErr.Error(), "déjà actif") {
		cancel()
		t.Fatalf("second daemon non refusé: %v", secondErr)
	}

	job, err := client.createWake("two", "api-test", true)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		job, err = client.job(job.ID)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if terminalJobState(job.State) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.State != "completed" || job.Actor != "api-test" {
		cancel()
		t.Fatalf("job daemon inattendu: %#v", job)
	}
	if packets != 0 {
		cancel()
		t.Fatalf("le dry-run a émis %d paquet(s)", packets)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	reloaded, err := openJobs(statePath)
	if err != nil {
		t.Fatal(err)
	}
	persisted, ok := reloaded.get(job.ID)
	if !ok || persisted.State != "completed" {
		t.Fatalf("job non persisté: %#v, présent=%v", persisted, ok)
	}
}

func waitForDaemon(t *testing.T, client daemonClient) daemonStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		status, err := client.status()
		if err == nil {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon inaccessible: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func reserveTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
