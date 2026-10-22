package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func testConfig(t *testing.T) *Config {
	t.Helper()
	dir := testDir(t)
	f := false
	c := &Config{
		Version:  1,
		Global:   Defaults{Port: 9, TransportOrder: []string{"unicast", "directed_broadcast"}},
		Networks: map[string]Network{"lan": {CIDR: "127.0.0.0/8", AllowUnicast: true}},
		Groups:   map[string]Group{"core": {Priority: 10, Servers: []string{"one"}}},
		Servers: map[string]Server{
			"one": {Name: "One", MAC: "02:00:00:00:00:01", IP: "127.0.0.1", Network: "lan", Enabled: &f},
			"two": {Name: "Two", MAC: "02:00:00:00:00:02", IP: "127.0.0.2", Network: "lan"},
		},
		API:       APIConfig{Listen: "127.0.0.1:8080", RatePerMinute: 60},
		Storage:   StorageConfig{StateFile: filepath.Join(dir, "state.json"), AuditFile: filepath.Join(dir, "audit.jsonl")},
		Discovery: DiscoveryConfig{AllowedSubnets: []string{"127.0.0.0/8"}, Safeguards: Safeguards{MaxPackets: 2, RatePPS: 1000, Timeout: Duration(time.Second)}},
		UPS:       map[string]UPSConfig{}, Relays: map[string]RelayConfig{},
	}
	return c
}

func testDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", "wol-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestBashCompletionContainsCommandsAndSubcommands(t *testing.T) {
	for _, expected := range []string{
		`compgen -W "setup update list status wake config token network ups jobs automation menu`,
		`config) COMPREPLY=( $(compgen -W "check edit network machine group nut --help -h"`,
		`wake) COMPREPLY=( $(compgen -W "--dry-run --verification-delay --group --all --help -h"`,
		`automation) COMPREPLY=( $(compgen -W "status --help -h"`,
		`version) COMPREPLY=( $(compgen -W "--channel --source-revision --commit --help -h"`,
		`complete -F _wol wol`,
	} {
		if !strings.Contains(bashCompletion, expected) {
			t.Fatalf("autocomplétion Bash incomplète, fragment absent: %s", expected)
		}
	}
}

func TestOpenAPIDocumentIsValidYAMLAndExposesAutomations(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("OpenAPI YAML invalide: %v", err)
	}
	text := string(content)
	for _, expected := range []string{"version: 1.2.1", "/automations:"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("OpenAPI incomplet, fragment absent: %s", expected)
		}
	}
}

func TestHelpDoesNotLoadConfiguration(t *testing.T) {
	missing := filepath.Join(testDir(t), "absent.yaml")
	if err := run([]string{"--config", missing, "--help"}); err != nil {
		t.Fatalf("aide générale: %v", err)
	}
	commands := [][]string{
		{"setup", "--help"}, {"update", "--help"}, {"completion", "bash", "--help"},
		{"list", "--help"}, {"status", "--help"}, {"wake", "--help"},
		{"config", "--help"}, {"config", "check", "--help"}, {"config", "edit", "--help"},
		{"config", "network", "--help"}, {"config", "machine", "--help"},
		{"config", "group", "--help"}, {"config", "nut", "--help"},
		{"config", "discovery", "--help"}, {"token", "create", "--help"},
		{"token", "list", "--help"}, {"token", "revoke", "--help"}, {"network", "test", "--help"},
		{"network", "discover", "--help"}, {"ups", "status", "--help"},
		{"ups", "test", "--help"}, {"jobs", "--help"}, {"automation", "status", "--help"},
		{"menu", "--help"}, {"api", "serve", "--help"}, {"relay", "serve", "--help"},
		{"relay", "check", "--help"}, {"version", "--help"},
	}
	for _, command := range commands {
		args := append([]string{"--config", missing}, command...)
		if err := run(args); err != nil {
			t.Fatalf("aide %q: %v", strings.Join(command, " "), err)
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("l'aide a créé ou modifié la configuration: %v", err)
	}
}

func TestTokenListAndRevokeNeverExposeSecretOrHash(t *testing.T) {
	cfg := testConfig(t)
	cfg.API.TokensFile = filepath.Join(testDir(t), "tokens.json")
	hash := strings.Repeat("a", 64)
	if err := writeTokenFile(cfg.API.TokensFile, tokenFile{Tokens: []TokenRecord{{ID: "reader", Hash: hash, Permissions: []string{"read"}}}}); err != nil {
		t.Fatal(err)
	}
	var listed bytes.Buffer
	if err := tokenCLIWithIO(cfg, []string{"list"}, strings.NewReader(""), &listed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.String(), "reader") || !strings.Contains(listed.String(), "read") || strings.Contains(listed.String(), hash) {
		t.Fatalf("liste dangereuse ou incomplète: %s", listed.String())
	}
	var revoked bytes.Buffer
	if err := tokenCLIWithIO(cfg, []string{"revoke", "--id", "reader"}, strings.NewReader("o\n"), &revoked); err != nil {
		t.Fatal(err)
	}
	f, err := readTokenFile(cfg.API.TokensFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Tokens) != 1 || !f.Tokens[0].Disabled || f.Tokens[0].Hash != hash {
		t.Fatalf("révocation incorrecte: %#v", f)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(cfg.API.TokensFile); err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("permissions jetons: %v %v", info, err)
		}
	}
}

func TestRunningAPIReloadsCreatedAndRevokedTokensImmediately(t *testing.T) {
	cfg := testConfig(t)
	cfg.API.TokensFile = filepath.Join(testDir(t), "tokens.json")
	if err := writeTokenFile(cfg.API.TokensFile, tokenFile{}); err != nil {
		t.Fatal(err)
	}
	store, err := openJobs(cfg.Storage.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	server := &APIServer{cfg: cfg, jobs: store, engine: &Engine{cfg: cfg, audit: &Auditor{}}, tokenPath: cfg.API.TokensFile, rates: map[string]rateEntry{}}

	var created bytes.Buffer
	if err := tokenCLIWithIO(cfg, []string{"create", "--id", "live", "--permissions", "read"}, strings.NewReader(""), &created); err != nil {
		t.Fatal(err)
	}
	plain := strings.TrimSpace(strings.TrimPrefix(created.String(), "JETON (affiché une seule fois):"))
	if code := apiHealthCode(server, plain); code != http.StatusOK {
		t.Fatalf("jeton créé non chargé immédiatement: HTTP %d", code)
	}

	var other bytes.Buffer
	if err := tokenCLIWithIO(cfg, []string{"create", "--id", "other", "--permissions", "read"}, strings.NewReader(""), &other); err != nil {
		t.Fatal(err)
	}
	otherPlain := strings.TrimSpace(strings.TrimPrefix(other.String(), "JETON (affiché une seule fois):"))

	var revoked bytes.Buffer
	if err := tokenCLIWithIO(cfg, []string{"revoke", "--id", "live"}, strings.NewReader("o\n"), &revoked); err != nil {
		t.Fatal(err)
	}
	if code := apiHealthCode(server, plain); code != http.StatusUnauthorized {
		t.Fatalf("jeton révoqué encore accepté: HTTP %d", code)
	}
	if code := apiHealthCode(server, otherPlain); code != http.StatusOK {
		t.Fatalf("jeton non concerné invalidé: HTTP %d", code)
	}
}

func TestTokenRevocationIsAtomicUnderConcurrentRequests(t *testing.T) {
	cfg := testConfig(t)
	cfg.API.TokensFile = filepath.Join(testDir(t), "tokens.json")
	plain, hash, err := generateToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTokenFile(cfg.API.TokensFile, tokenFile{Tokens: []TokenRecord{{ID: "live", Hash: hash, Permissions: []string{"read"}}}}); err != nil {
		t.Fatal(err)
	}
	server := &APIServer{cfg: cfg, tokenPath: cfg.API.TokensFile, rates: map[string]rateEntry{}}
	done := make(chan int, 32)
	for i := 0; i < cap(done); i++ {
		go func() { done <- apiHealthCode(server, plain) }()
	}
	var output bytes.Buffer
	if err := tokenCLIWithIO(cfg, []string{"revoke", "--id", "live"}, strings.NewReader("o\n"), &output); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cap(done); i++ {
		code := <-done
		if code != http.StatusOK && code != http.StatusUnauthorized {
			t.Fatalf("réponse intermédiaire incohérente: HTTP %d", code)
		}
	}
	for i := 0; i < 10; i++ {
		if code := apiHealthCode(server, plain); code != http.StatusUnauthorized {
			t.Fatalf("révocation finale non appliquée: HTTP %d", code)
		}
	}
}

func apiHealthCode(server *APIServer, token string) int {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	server.handler().ServeHTTP(response, request)
	return response.Code
}

func TestAPIBannerIsDiscoverableAndContainsNoSecret(t *testing.T) {
	cfg := testConfig(t)
	var output bytes.Buffer
	printAPIBanner(&output, cfg)
	for _, expected := range []string{"127.0.0.1:8080", "/api/v1", "/api/v1/health", "Authorization: Bearer <jeton>", "api/openapi.yaml", "Ctrl+C"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("bannière API incomplète (%s): %s", expected, output.String())
		}
	}
}

func TestRealWakeSelectionRequiresExplicitKnownMachine(t *testing.T) {
	cfg := testConfig(t)
	var output bytes.Buffer
	if _, ok := selectMenuServer(bufio.NewReader(strings.NewReader("\n")), &output, cfg); ok {
		t.Fatal("une réponse vide a sélectionné une machine")
	}
	if id, ok := selectMenuServer(bufio.NewReader(strings.NewReader("99\ntwo\n")), &output, cfg); !ok || id != "two" {
		t.Fatalf("sélection explicite échouée: %q %t", id, ok)
	}
	if !strings.Contains(output.String(), "One") || !strings.Contains(output.String(), "Two") {
		t.Fatalf("noms de machines absents: %s", output.String())
	}
}

func TestSimulationSelectionUsesNumberIdentifierAndContextHelp(t *testing.T) {
	cfg := testConfig(t)
	var output bytes.Buffer
	id, ok := selectMenuServer(bufio.NewReader(strings.NewReader("?\n2\n")), &output, cfg)
	if !ok || id != "two" {
		t.Fatalf("sélection après aide: %q %t", id, ok)
	}
	if !strings.Contains(output.String(), "numéro affiché") || !strings.Contains(output.String(), "One") {
		t.Fatalf("aide ou liste absente: %s", output.String())
	}
}

func TestMenuAnnouncesPreflightCheckBeforeRealWakeConfirmation(t *testing.T) {
	cfg := testConfig(t)
	cfg.Global.Checks = []Check{{Type: "tcp", Port: 1, Timeout: Duration(10 * time.Millisecond)}}
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
	if _, err := inputWriter.WriteString("8\n2\nn\n0\n"); err != nil {
		t.Fatal(err)
	}
	_ = inputWriter.Close()
	if err := menu("", cfg, &Engine{cfg: cfg, audit: &Auditor{}}, false); err != nil {
		t.Fatal(err)
	}
	_ = outputWriter.Close()
	text, err := io.ReadAll(outputReader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "Contrôle préalable de two en cours, délai maximal 45 secondes…") {
		t.Fatalf("progression absente:\n%s", text)
	}
}

func TestDisabledChecksReturnUnknownImmediately(t *testing.T) {
	cfg := testConfig(t)
	server := cfg.Servers["two"]
	server.ChecksDisabled = true
	server.Verification.InitialDelay = durationPointer(90 * time.Second)
	cfg.Servers["two"] = server
	start := time.Now()
	result := checkServer(context.Background(), cfg, "two")
	if result.State != "unknown" || result.Note != "aucune vérification configurée" || len(result.Results) != 0 {
		t.Fatalf("résultat inattendu: %#v", result)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatalf("status sans contrôle a attendu %s", time.Since(start))
	}
}

func TestTokenCreateHelpShowsBearerUsage(t *testing.T) {
	help := commandHelp["token create"]
	for _, expected := range []string{"Authorization: Bearer $TOKEN", "127.0.0.1:8080/api/v1/health"} {
		if !strings.Contains(help, expected) {
			t.Fatalf("aide token create sans %q:\n%s", expected, help)
		}
	}
}

func TestHumanMenuPresentationAvoidsRawJSONAndNanoseconds(t *testing.T) {
	cfg := testConfig(t)
	var output bytes.Buffer
	printMenuMachines(&output, cfg)
	printNetworks(&output, cfg.Networks)
	printGroups(&output, map[string]Group{"restore": {
		Priority:     1,
		PowerRestore: &PowerRestorePolicy{Enabled: true, UPS: "main", StableFor: Duration(30 * time.Second), PollInterval: Duration(5 * time.Second), Mode: "sequential", FailurePolicy: "retry", RetryLimit: 3, RetryInterval: Duration(10 * time.Second)},
	}})
	printMenuUPS(&output, map[string]UPSStatus{"main": {RawStatus: "OL", Battery: 75, BatteryKnown: true}})
	printMenuAutomations(&output,
		[]restorePlan{{Group: "restore", UPS: "main", StableFor: 30 * time.Second, PollInterval: 5 * time.Second, Mode: "sequential", FailurePolicy: "retry", RetryLimit: 3, RetryInterval: 10 * time.Second}},
		[]PowerCycle{{UPS: "main", State: "waiting", Battery: 75, BatteryKnown: true}},
		[]Job{{ID: "job-1", Kind: "wake", Target: "two", State: "running"}},
	)
	text := output.String()
	for _, expected := range []string{"délais=", "port=9", "stabilité=30s", "fréquence=5s", "batterie=75%", "état=running"} {
		if !strings.Contains(text, expected) {
			t.Errorf("%q absent de la sortie humaine:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "stable_for_ns") || strings.Contains(text, `{"`) {
		t.Fatalf("sortie technique brute dans le menu:\n%s", text)
	}
}

func TestHumanWakeResultsAvoidRawJSON(t *testing.T) {
	var output bytes.Buffer
	printWakeResults(&output, []WakeResult{{
		Server:      "two",
		Transport:   "local_broadcast",
		Destination: "192.0.2.255:9",
		Packet:      "dry_run",
		Note:        "simulation: aucun paquet émis",
	}})
	text := output.String()
	for _, expected := range []string{"two", "simulation, aucun paquet émis", "transport=local_broadcast", "destination=192.0.2.255:9", "vérifié=false"} {
		if !strings.Contains(text, expected) {
			t.Errorf("%q absent de la sortie humaine: %s", expected, text)
		}
	}
	if strings.Contains(text, `{"`) || strings.Contains(text, `"server"`) {
		t.Fatalf("JSON brut dans la sortie humaine: %s", text)
	}
}

func TestConfigHelpContent(t *testing.T) {
	for _, section := range []string{"network", "machine", "group", "nut"} {
		var output bytes.Buffer
		if err := printCommandHelp(&output, []string{"config", section, "--help"}); err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{"Usage:", "Actions:", "Paramètres:", "Contraintes:"} {
			if !strings.Contains(output.String(), expected) {
				t.Fatalf("aide %s incomplète: %q absent", section, expected)
			}
		}
	}
}

func TestContextualHelpCoversV12Functions(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"config", "group", "--help"}, "seuils bas/haut"},
		{[]string{"config", "nut", "--help"}, "strictement en lecture"},
		{[]string{"automation", "status", "--help"}, "wol api serve"},
		{[]string{"wake", "--help"}, "dépendances"},
		{[]string{"jobs", "--help"}, "revalidation_required"},
	} {
		var output bytes.Buffer
		if err := printCommandHelp(&output, test.args); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), test.want) {
			t.Fatalf("aide %q incomplète: %q absent", strings.Join(test.args, " "), test.want)
		}
	}

	var output bytes.Buffer
	for _, topic := range []string{"setup", "network", "machine", "group", "nut", "menu"} {
		printContextHelp(&output, topic)
	}
	for _, expected := range []string{"Rien n'est écrit", "transports WoL", "simulation", "séquentiel", "strictement lu", "réveil réel"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("aide interactive incomplète: %q absent", expected)
		}
	}
}

func sandboxBlocksNetwork(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "interdite par ses autorisations") || strings.Contains(strings.ToLower(err.Error()), "permission denied"))
}

func TestMagicPacket(t *testing.T) {
	mac, _ := net.ParseMAC("00:11:22:33:44:55")
	p := magicPacket(mac)
	if len(p) != 102 || string(p[:6]) != strings.Repeat("\xff", 6) {
		t.Fatalf("paquet magique invalide: %d octets", len(p))
	}
	for i := 6; i < len(p); i += 6 {
		if string(p[i:i+6]) != string(mac) {
			t.Fatalf("répétition MAC invalide à %d", i)
		}
	}
}

func TestValidationRejectsCycleDuplicatesAndPublicScan(t *testing.T) {
	c := testConfig(t)
	c.Servers["one"] = Server{Name: "One", MAC: "02:00:00:00:00:01", IP: "127.0.0.1", Network: "lan", Dependencies: []string{"two"}}
	c.Servers["two"] = Server{Name: "Two", MAC: "02:00:00:00:00:01", IP: "127.0.0.2", Network: "lan", Dependencies: []string{"one"}}
	c.Discovery.ActiveEnabled = true
	c.Discovery.AllowedSubnets = []string{"8.8.8.0/24"}
	err := c.Validate()
	if err == nil {
		t.Fatal("configuration dangereuse acceptée")
	}
	for _, want := range []string{"dépendance circulaire", "MAC dupliquée", "réseau public interdit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("erreur manquante %q: %v", want, err)
		}
	}
}

func TestStrictYAML(t *testing.T) {
	p := filepath.Join(testDir(t), "bad.yaml")
	if err := os.WriteFile(p, []byte("version: 1\nunknown_field: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(p); err == nil || !strings.Contains(err.Error(), "unknown_field") {
		t.Fatalf("champ inconnu accepté: %v", err)
	}
}

func TestTransportRestrictionsAndDryRun(t *testing.T) {
	c := testConfig(t)
	p, err := transportPlan(c, "two")
	if err != nil || p[0].Name != "unicast" || p[0].State != "selected" {
		t.Fatalf("plan inattendu: %#v %v", p, err)
	}
	c.Networks["lan"] = Network{CIDR: "127.0.0.0/8", AllowDirectedBroadcast: true, TransportOrder: []string{"directed_broadcast"}}
	p, err = transportPlan(c, "two")
	if err != nil || p[0].Destination != "127.255.255.255:9" {
		t.Fatalf("broadcast dirigé: %#v %v", p, err)
	}
	r := activeDiscovery(context.Background(), c, true)
	for _, x := range r.Tests {
		if x.Kind != "guard" && x.Packet != "dry_run" {
			t.Fatalf("dry-run a produit %s", x.Packet)
		}
	}
}

func TestConfigurationInheritance(t *testing.T) {
	c := testConfig(t)
	one := 1
	two := 2
	n := c.Networks["lan"]
	n.Checks = []Check{{Type: "tcp", Port: 80}}
	n.WakeRetries = &one
	n.TransportOrder = []string{"unicast"}
	c.Networks["lan"] = n
	g := c.Groups["core"]
	g.Checks = []Check{{Type: "tcp", Port: 443}}
	g.WakeRetries = &two
	g.TransportOrder = []string{"directed_broadcast"}
	c.Groups["core"] = g
	s := c.Servers["one"]
	s.Checks = []Check{{Type: "tcp", Port: 22}}
	s.Transports = []string{"unicast"}
	c.Servers["one"] = s
	if got := c.effectiveChecks("one")[0].Port; got != 22 {
		t.Fatalf("checks serveur non prioritaire: %d", got)
	}
	if got := c.effectiveTransportOrder("one")[0]; got != "unicast" {
		t.Fatalf("transport serveur non prioritaire: %s", got)
	}
	_, retries, _ := c.effectiveTiming("one")
	if retries != 2 {
		t.Fatalf("priorité groupe non appliquée: %d", retries)
	}
}

func TestChecksDisabledHierarchyAndExplicitOverride(t *testing.T) {
	c := testConfig(t)
	c.Global.Checks = []Check{{Type: "tcp", Port: 22}}
	n := c.Networks["lan"]
	n.ChecksDisabled = true
	c.Networks["lan"] = n
	if checks, disabled := c.effectiveCheckPolicy("two"); !disabled || len(checks) != 0 {
		t.Fatalf("désactivation réseau ignorée: %#v disabled=%t", checks, disabled)
	}
	s := c.Servers["two"]
	s.Checks = []Check{{Type: "tcp", Port: 443}}
	c.Servers["two"] = s
	if checks, disabled := c.effectiveCheckPolicy("two"); disabled || len(checks) != 1 || checks[0].Port != 443 {
		t.Fatalf("surcharge machine ignorée: %#v disabled=%t", checks, disabled)
	}

	n.ChecksDisabled = false
	c.Networks["lan"] = n
	g := c.Groups["pair"]
	g.ChecksDisabled = true
	c.Groups["pair"] = g
	s = c.Servers["two"]
	s.Checks = nil
	s.Groups = []string{"pair"}
	c.Servers["two"] = s
	if checks, disabled := c.effectiveCheckPolicy("two"); !disabled || len(checks) != 0 {
		t.Fatalf("désactivation groupe ignorée: %#v disabled=%t", checks, disabled)
	}
}

func TestChecksDisabledRejectsExplicitChecksAtSameLevel(t *testing.T) {
	c := testConfig(t)
	s := c.Servers["two"]
	s.ChecksDisabled = true
	s.Checks = []Check{{Type: "tcp", Port: 22}}
	c.Servers["two"] = s
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "checks et checks_disabled sont incompatibles") {
		t.Fatalf("configuration contradictoire acceptée: %v", err)
	}
}

func TestJobPersistenceCancellationAndRestart(t *testing.T) {
	path := filepath.Join(testDir(t), "state.json")
	s, err := openJobs(path)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	j, err := s.create("test", "x", func(ctx context.Context) (any, error) { close(started); <-ctx.Done(); return nil, ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, duplicateErr := s.create("test", "x", func(context.Context) (any, error) { return nil, nil }); duplicateErr == nil {
		t.Fatal("tâche active dupliquée acceptée")
	}
	if err = s.cancelJob(j.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		x, _ := s.get(j.ID)
		if x.State == "cancelled" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f jobFile
	if json.Unmarshal(b, &f) != nil || f.Jobs[j.ID].State != "cancelled" {
		t.Fatalf("état non persisté: %s", b)
	}
	f.Jobs[j.ID].State = "running"
	b, _ = json.Marshal(f)
	_ = os.WriteFile(path, b, 0600)
	s2, err := openJobs(path)
	if err != nil {
		t.Fatal(err)
	}
	x, _ := s2.get(j.ID)
	if x.State != "running" {
		t.Fatalf("une simple lecture a modifié la tâche: %s", x.State)
	}
	if err := s2.markInterrupted(); err != nil {
		t.Fatal(err)
	}
	x, _ = s2.get(j.ID)
	if x.State != "revalidation_required" {
		t.Fatalf("reprise dangereuse: %s", x.State)
	}
	if err := s2.resume(j.ID, func(context.Context) (any, error) { return "revalidé", nil }); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		x, _ = s2.get(j.ID)
		if x.State == "completed" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("tâche non reprise: %s", x.State)
}

func TestCancellationIsTerminalWhenWorkerReturnsSuccess(t *testing.T) {
	path := filepath.Join(testDir(t), "state.json")
	store, err := openJobs(path)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	job, err := store.create("network_discovery", "true", func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		return map[string]string{"late": "result"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := store.cancelJob(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.cancelJob(job.ID); err == nil {
		t.Fatal("double annulation acceptée")
	}
	time.Sleep(20 * time.Millisecond)
	current, _ := store.get(job.ID)
	if current.State != "cancelled" {
		t.Fatalf("résultat tardif ayant remplacé l'annulation: %#v", current)
	}
	reloaded, err := openJobs(path)
	if err != nil {
		t.Fatal(err)
	}
	persisted, _ := reloaded.get(job.ID)
	if persisted.State != "cancelled" {
		t.Fatalf("annulation non persistée: %#v", persisted)
	}
}

func TestNaturalCompletionWinsBeforeCancellation(t *testing.T) {
	store, err := openJobs(filepath.Join(testDir(t), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.create("test", "fast", func(context.Context) (any, error) { return "done", nil })
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		current, _ := store.get(job.ID)
		if current.State == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job non terminé: %#v", current)
		}
		time.Sleep(time.Millisecond)
	}
	if err := store.cancelJob(job.ID); err == nil {
		t.Fatal("annulation d'un job terminé acceptée")
	}
	store.update(job.ID, "failed", "résultat tardif", nil)
	current, _ := store.get(job.ID)
	if current.State != "completed" {
		t.Fatalf("état terminal remplacé: %#v", current)
	}
}

func TestCompletionCancellationRaceHasOneTerminalWinner(t *testing.T) {
	for i := 0; i < 50; i++ {
		store, err := openJobs(filepath.Join(testDir(t), "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		started := make(chan struct{})
		release := make(chan struct{})
		job, err := store.create("race", fmt.Sprint(i), func(context.Context) (any, error) {
			close(started)
			<-release
			return "done", nil
		})
		if err != nil {
			t.Fatal(err)
		}
		<-started
		cancelled := make(chan error, 1)
		go func() { cancelled <- store.cancelJob(job.ID) }()
		close(release)
		cancelErr := <-cancelled
		deadline := time.Now().Add(time.Second)
		for {
			current, _ := store.get(job.ID)
			if terminalJobState(current.State) {
				if cancelErr == nil && current.State != "cancelled" {
					t.Fatalf("annulation acceptée puis remplacée par %s", current.State)
				}
				if cancelErr != nil && current.State != "completed" {
					t.Fatalf("fin naturelle gagnante dans l'état %s: %v", current.State, cancelErr)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("course sans état terminal: %#v", current)
			}
			time.Sleep(time.Millisecond)
		}
	}
}

func TestAPICancellationOfActiveDiscoveryIsTerminal(t *testing.T) {
	cfg := testConfig(t)
	store, err := openJobs(cfg.Storage.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	job, err := store.create("network_discovery", "true", func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		return map[string]string{"status": "context canceled"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	srv := &APIServer{cfg: cfg, jobs: store, rates: map[string]rateEntry{}}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+job.ID+"/cancel", nil)
	r = r.WithContext(context.WithValue(r.Context(), principalKey, principal{ID: "admin", permissions: map[string]bool{"admin": true}}))
	w := httptest.NewRecorder()
	srv.job(w, r)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), "cancellation_requested") {
		t.Fatalf("annulation API: %d %s", w.Code, w.Body.String())
	}
	time.Sleep(20 * time.Millisecond)
	current, _ := store.get(job.ID)
	if current.State != "cancelled" {
		t.Fatalf("découverte annulée terminée dans l'état %s", current.State)
	}

	w = httptest.NewRecorder()
	srv.job(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("double annulation API: %d %s", w.Code, w.Body.String())
	}
}

func TestActiveDiscoveryIsNeverResumed(t *testing.T) {
	c := testConfig(t)
	store, _ := openJobs(c.Storage.StateFile)
	now := time.Now().UTC()
	store.jobs["scan"] = &Job{ID: "scan", Kind: "network_discovery", Target: "true", State: "revalidation_required", Created: now, Updated: now}
	resumeJobs(store, &Engine{cfg: c, audit: &Auditor{}})
	j, _ := store.get("scan")
	if j.State != "failed" || !strings.Contains(j.Message, "interdite") {
		t.Fatalf("découverte active reprise: %#v", j)
	}
}

func TestRelayClientRequiresAuthenticationMaterial(t *testing.T) {
	dir := testDir(t)
	_, caKey, _ := ed25519.GenerateKey(rand.Reader)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTpl, caTpl, caKey.Public(), caKey)
	caPath := filepath.Join(dir, "ca.pem")
	_ = os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600)
	_, clientKey, _ := ed25519.GenerateKey(rand.Reader)
	clientTpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "manager"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	clientDER, _ := x509.CreateCertificate(rand.Reader, clientTpl, caTpl, clientKey.Public(), caKey)
	certPath := filepath.Join(dir, "client.pem")
	keyPath := filepath.Join(dir, "client.key")
	_ = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER}), 0600)
	keyDER, _ := x509.MarshalPKCS8PrivateKey(clientKey)
	_ = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600)
	cl, err := relayHTTPClient(RelayConfig{CACert: caPath, ClientCert: certPath, ClientKey: keyPath})
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig := cl.Transport.(*http.Transport).TLSClientConfig
	if len(tlsConfig.Certificates) != 1 || tlsConfig.RootCAs == nil {
		t.Fatal("matériel mTLS non chargé")
	}
}

func TestAPIPermissions(t *testing.T) {
	c := testConfig(t)
	store, _ := openJobs(c.Storage.StateFile)
	plain := "reader-secret"
	sum := sha256.Sum256([]byte(plain))
	engine := &Engine{cfg: c, audit: &Auditor{}}
	manager := newAutomationManager(c, store, engine)
	srv := &APIServer{cfg: c, jobs: store, engine: engine, tokens: []TokenRecord{{ID: "reader", Hash: hex.EncodeToString(sum[:]), Permissions: []string{"read"}}}, rates: map[string]rateEntry{}, automations: manager}
	request := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer "+plain)
		w := httptest.NewRecorder()
		srv.handler().ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/v1/health"); w.Code != http.StatusOK {
		t.Fatalf("lecture refusée: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/automations"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"cycles"`) {
		t.Fatalf("état des automatismes indisponible: %d %s", w.Code, w.Body.String())
	}
	if w := request("POST", "/api/v1/servers/two/wake"); w.Code != http.StatusForbidden {
		t.Fatalf("wake sans permission accepté: %d", w.Code)
	}
	if w := request("POST", "/api/v1/networks/discovery"); w.Code != http.StatusForbidden {
		t.Fatalf("diagnostic admin sans permission accepté: %d", w.Code)
	}
	r := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	srv.handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("requête anonyme acceptée: %d", w.Code)
	}
}

func TestNUTStates(t *testing.T) {
	for raw, want := range map[string]string{"OL": "OL", "OB": "OB", "OB LB": "LB", "FSD OB": "FSD", "": "unknown"} {
		r := UPSStatus{State: "unknown"}
		applyNUTVars(&r, map[string]string{"ups.status": raw, "battery.charge": "50", "battery.runtime": "60"})
		if r.State != want {
			t.Fatalf("NUT %q: got %s want %s", raw, r.State, want)
		}
		if !r.BatteryKnown || !r.RuntimeKnown {
			t.Fatalf("mesures NUT valides non reconnues pour %q", raw)
		}
	}
	invalid := UPSStatus{State: "unknown"}
	applyNUTVars(&invalid, map[string]string{"ups.status": "OL", "battery.charge": "inconnue", "battery.runtime": "-1"})
	if invalid.BatteryKnown || invalid.RuntimeKnown {
		t.Fatal("mesure NUT invalide interprétée comme fiable")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan error, 1)
	go func() {
		cn, acceptErr := ln.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer cn.Close()
		buf := make([]byte, 256)
		_, _ = cn.Read(buf)
		_, acceptErr = cn.Write([]byte("BEGIN LIST VAR ups\nVAR ups ups.status \"OL\"\nVAR ups battery.charge \"87\"\nVAR ups battery.runtime \"900\"\nEND LIST VAR ups\n"))
		done <- acceptErr
	}()
	r := nutStatus(context.Background(), "main", UPSConfig{Address: ln.Addr().String(), Name: "ups", Timeout: Duration(time.Second)})
	if sandboxBlocksNetwork(errors.New(r.Error)) {
		t.Skipf("sockets locaux bloqués par le bac à sable: %s", r.Error)
	}
	if r.State != "OL" || r.Battery != 87 || r.Runtime != 900 {
		t.Fatalf("état NUT incorrect: %#v", r)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
