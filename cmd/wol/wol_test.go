package main

import (
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
		`config) COMPREPLY=( $(compgen -W "check edit network machine group nut"`,
		`wake) COMPREPLY=( $(compgen -W "--dry-run --verification-delay --group --all"`,
		`automation) COMPREPLY=( $(compgen -W "status"`,
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
	for _, expected := range []string{"version: 1.2.0", "/automations:"} {
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
	for _, section := range []string{"network", "machine", "group", "nut"} {
		if err := run([]string{"--config", missing, "config", section, "--help"}); err != nil {
			t.Fatalf("aide config %s: %v", section, err)
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("l'aide a créé ou modifié la configuration: %v", err)
	}
}

func TestConfigHelpContent(t *testing.T) {
	for _, section := range []string{"network", "machine", "group", "nut"} {
		var output bytes.Buffer
		if err := printConfigHelp(&output, section); err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{"Actions:", "Paramètres", "Valeurs par défaut:", "Contraintes:", "Exemple:", "Mode interactif:", "Mode non interactif:"} {
			if !strings.Contains(output.String(), expected) {
				t.Fatalf("aide %s incomplète: %q absent", section, expected)
			}
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

func TestSendMagicUnicast(t *testing.T) {
	ln, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	received := make(chan error, 1)
	go func() {
		_ = ln.SetReadDeadline(time.Now().Add(3 * time.Second))
		b := make([]byte, 200)
		n, _, err := ln.ReadFromUDP(b)
		if err == nil && n != 102 {
			err = &net.AddrError{Err: "taille inattendue", Addr: ln.LocalAddr().String()}
		}
		received <- err
	}()
	packet := make([]byte, 102)
	if err := sendMagic(ln.LocalAddr().String(), "127.0.0.1", false, packet); err != nil {
		t.Fatal(err)
	}
	if err := <-received; sandboxBlocksNetwork(err) || (runtime.GOOS == "windows" && isTimeout(err)) {
		t.Skipf("sockets locaux bloqués par le bac à sable: %v", err)
	} else if err != nil {
		t.Fatal(err)
	}
}

func isTimeout(err error) bool { n, ok := err.(net.Error); return ok && n.Timeout() }

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
