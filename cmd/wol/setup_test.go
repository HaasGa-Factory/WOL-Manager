package main

import (
	"bufio"
	"bytes"
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
		"test-pc", "Machine test", "02:00:00:00:50:10", "192.168.50.10", "lan", "", "", "1", "", "90s", "180s", "5s", "", "", "4",
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

func TestGuidedSetupContextHelpDoesNotWrite(t *testing.T) {
	path := filepath.Join(testDir(t), "wol.yaml")
	var output strings.Builder
	if err := setupWithIO(path, strings.NewReader("?\n3\n?\n6\n"), &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"NUT reste facultatif", "Rien n'est écrit", "Configuration existante conservée"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("aide guidée incomplète: %q absent", expected)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("l'aide guidée a écrit la configuration: %v", err)
	}
}

func TestGuidedGlobalVerification(t *testing.T) {
	cfg := testConfig(t)
	input := bufio.NewReader(strings.NewReader("10s\n3m\n5s\n\n\nany\n"))
	var output strings.Builder
	if !configureGlobalVerification(input, &output, cfg) {
		t.Fatalf("configuration globale non modifiée: %s", output.String())
	}
	initialDelay, timeout, interval := cfg.effectiveVerification("two")
	if initialDelay != 10*time.Second || timeout != 3*time.Minute || interval != 5*time.Second {
		t.Fatalf("délais globaux incorrects: %s, %s, %s", initialDelay, timeout, interval)
	}
}

func TestQuestionMarkNeverChangesInteractiveDefaults(t *testing.T) {
	var output strings.Builder
	if got := promptDefault(bufio.NewReader(strings.NewReader("?\n\n")), &output, "Identifiant du réseau", "lan"); got != "lan" {
		t.Fatalf("texte modifié par ?: %q", got)
	}
	if got := promptBool(bufio.NewReader(strings.NewReader("?\n\n")), &output, "Autoriser le broadcast local", true); !got {
		t.Fatal("booléen modifié par ?")
	}
	d := Duration(30 * time.Second)
	if got, err := promptDurationOverride(bufio.NewReader(strings.NewReader("?\n\n")), &output, "Durée maximale", &d, false); err != nil || got == nil || *got != d {
		t.Fatalf("durée modifiée par ?: %v %v", got, err)
	}
	n := 4
	if got, err := promptIntOverride(bufio.NewReader(strings.NewReader("?\n\n")), &output, "Nombre maximal", &n); err != nil || got == nil || *got != n {
		t.Fatalf("nombre modifié par ?: %v %v", got, err)
	}
	if got := promptKnownIDs(bufio.NewReader(strings.NewReader("?\n\n")), &output, "Machines", []string{"nas"}, map[string]bool{"nas": true}); len(got) != 1 || got[0] != "nas" {
		t.Fatalf("sélection modifiée par ?: %v", got)
	}
	if got, err := chooseDuration(bufio.NewReader(strings.NewReader("?\n1\n")), &output, []time.Duration{30 * time.Second}, time.Minute, false); err != nil || got != Duration(30*time.Second) {
		t.Fatalf("choix après ?: %v %v", got, err)
	}
	checks := []Check{{Type: "icmp", Timeout: Duration(time.Second)}}
	if got := promptChecks(bufio.NewReader(strings.NewReader("?\n\n")), &output, checks); len(got) != 1 || got[0].Type != "icmp" {
		t.Fatalf("sélection de contrôle modifiée par ?: %#v", got)
	}
	if strings.Count(output.String(), "Aide:") < 5 {
		t.Fatalf("aides de champ absentes:\n%s", output.String())
	}
}

func TestGuidedDiscoveryExplainsAndConfiguresBothLocks(t *testing.T) {
	cfg := testConfig(t)
	cfg.Discovery.AllowedSubnets = []string{"192.168.10.0/24"}
	input := bufio.NewReader(strings.NewReader("?\no\n?\no\n\n\n\n\n?\no\n"))
	var output strings.Builder
	if !configureDiscovery(input, &output, cfg) {
		t.Fatalf("découverte non modifiée:\n%s", output.String())
	}
	if !cfg.Discovery.PassiveEnabled || !cfg.Discovery.ActiveEnabled || !cfg.API.ActiveDiscovery || !cfg.Discovery.Safeguards.RequireConfirmation {
		t.Fatalf("verrous incomplets: %#v %#v", cfg.Discovery, cfg.API)
	}
	if !strings.Contains(output.String(), "deux verrous") || !strings.Contains(output.String(), "Aide:") {
		t.Fatalf("explication novice absente: %s", output.String())
	}
}

func TestDiscoveryPersistsFromSectionAndReloads(t *testing.T) {
	path := filepath.Join(testDir(t), "wol.yaml")
	input := strings.NewReader("o\no\n192.168.1.0/24\n20\n2\n30s\no\no\n")
	var output strings.Builder
	if err := configSection(path, "discovery", input, &output); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Discovery.PassiveEnabled || !cfg.Discovery.ActiveEnabled || !cfg.API.ActiveDiscovery ||
		len(cfg.Discovery.AllowedSubnets) != 1 || cfg.Discovery.AllowedSubnets[0] != "192.168.1.0/24" ||
		cfg.Discovery.Safeguards.MaxPackets != 20 || cfg.Discovery.Safeguards.RatePPS != 2 ||
		cfg.Discovery.Safeguards.Timeout != Duration(30*time.Second) {
		t.Fatalf("découverte non persistée: %#v %#v", cfg.Discovery, cfg.API)
	}
	if report := activeDiscovery(context.Background(), cfg, true); !report.Active {
		t.Fatal("le dry-run actif n'est pas disponible après rechargement")
	}
	if !strings.Contains(output.String(), "Configuration enregistrée et validée") || !strings.Contains(output.String(), "Découverte réseau") {
		t.Fatalf("confirmation ou résumé absent:\n%s", output.String())
	}
}

func TestDiscoveryPersistsFromInitialSetup(t *testing.T) {
	path := filepath.Join(testDir(t), "wol.yaml")
	input := strings.NewReader(strings.Join([]string{
		"2",      // NUT: non
		"8",      // découverte
		"o", "o", // passive et active
		"192.168.2.0/24", "12", "1.5", "45s", "n",
		"5", "o", // résumé puis enregistrement
	}, "\n") + "\n")
	var output strings.Builder
	if err := setupWithIO(path, input, &output); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Discovery.ActiveEnabled || cfg.Discovery.Safeguards.MaxPackets != 12 || cfg.Discovery.Safeguards.RatePPS != 1.5 || cfg.Discovery.Safeguards.Timeout != Duration(45*time.Second) || cfg.API.ActiveDiscovery {
		t.Fatalf("découverte setup non persistée: %#v %#v", cfg.Discovery, cfg.API)
	}
}

func TestDiscoveryCancellationDoesNotModifyFile(t *testing.T) {
	path := filepath.Join(testDir(t), "wol.yaml")
	if err := os.WriteFile(path, []byte(minimalConfig), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	input := strings.NewReader("o\no\n192.168.3.0/24\n20\n2\n30s\no\nn\n")
	var output strings.Builder
	if err := configSection(path, "discovery", input, &output); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("une réponse négative a modifié le fichier")
	}
}

func TestFieldHelpMatchesTheRequestedSetting(t *testing.T) {
	cases := map[string][]string{
		"Attente avant la première vérification": {"délai après", "30s"},
		"Durée maximale des vérifications":       {"temps total maximal", "2m"},
		"Nombre maximal de vérifications":        {"nombre de contrôles", "timeout"},
		"Nouveaux Magic Packets après échec":     {"nouveaux envois", "distinct"},
		"Identifiant du serveur NUT":             {"serveur NUT", "ni d'une machine"},
		"Identifiant du groupe":                  {"wol wake --group"},
		"Adresse MAC":                            {"02:00:00:00:00:01"},
		"Dépendances machines":                   {"réveillées et vérifiées avant"},
		"Machines du groupe":                     {"wol wake --group"},
		"Ordre de démarrage":                     {"ordre explicite", "prérequis"},
		"Nombre maximal de nouvelles tentatives": {"0 désactive", "3 par défaut"},
		"Intervalle entre tentatives":            {"pause entre deux reprises", "30s"},
		"Seuil bas (%)":                          {"réarme"},
		"Seuil haut (%)":                         {"autorisant le réveil"},
		"Stabilité secteur":                      {"état OL", "30s"},
		"Fréquence NUT":                          {"lectures NUT"},
		"Mode (sequential=un par un)":            {"indépendantes en parallèle"},
		"Échec (stop/continue/retry)":            {"stop bloque", "continue_retry"},
		"Limite maximale de paquets":             {"plafond de paquets"},
		"Activer la reprise automatique NUT":     {"cycle de coupure", "Aucun ordre électrique"},
	}
	for label, fragments := range cases {
		help := fieldHelp(label)
		for _, fragment := range fragments {
			if !strings.Contains(help, fragment) {
				t.Errorf("aide %q sans %q: %s", label, fragment, help)
			}
		}
	}
}

func TestSummaryUsesEffectiveDefaultPort(t *testing.T) {
	cfg := testConfig(t)
	network := cfg.Networks["lan"]
	network.Port = 0
	cfg.Networks["lan"] = network
	var output strings.Builder
	showConfigSummary(&output, cfg)
	if !strings.Contains(output.String(), "port=9 (valeur par défaut)") || strings.Contains(output.String(), "port=0") {
		t.Fatalf("port effectif absent du résumé:\n%s", output.String())
	}
}

func TestGlobalVerificationDoesNotClaimToInherit(t *testing.T) {
	cfg := testConfig(t)
	var output strings.Builder
	configureGlobalVerification(bufio.NewReader(strings.NewReader("\n\n\n\n\nany\n")), &output, cfg)
	if strings.Contains(output.String(), "[hérité]") {
		t.Fatalf("un réglage global ne doit pas annoncer un parent inexistant:\n%s", output.String())
	}
}

func TestInvalidDurationRepromptsWithoutLosingPreviousValue(t *testing.T) {
	old := Duration(45 * time.Second)
	var output strings.Builder
	got, err := promptDurationOverride(bufio.NewReader(strings.NewReader("30\n2m\n")), &output, "Durée maximale", &old, false)
	if err != nil || got == nil || time.Duration(*got) != 2*time.Minute {
		t.Fatalf("nouvelle saisie non retenue: %v %v", got, err)
	}
	if !strings.Contains(output.String(), "Essayez 30s") {
		t.Fatalf("conseil de format absent: %s", output.String())
	}
}

func TestSingleIPv4InterfaceProvidesNetworkDefaults(t *testing.T) {
	previous := detectIPv4Interfaces
	detectIPv4Interfaces = func() []ipv4Interface {
		return []ipv4Interface{{Name: "eth0", Address: "192.168.10.2", CIDR: "192.168.10.0/24"}}
	}
	t.Cleanup(func() { detectIPv4Interfaces = previous })
	cfg := &Config{Version: 1, Networks: map[string]Network{}, Servers: map[string]Server{}, Groups: map[string]Group{}, UPS: map[string]UPSConfig{}, Relays: map[string]RelayConfig{}}
	cfg.defaults()
	var output strings.Builder
	if !addOrEditNetwork(bufio.NewReader(strings.NewReader("maison\n\n\n\n\n\n\n")), &output, cfg, "") {
		t.Fatalf("réseau non créé:\n%s", output.String())
	}
	network := cfg.Networks["maison"]
	if network.CIDR != "192.168.10.0/24" || network.Interface != "eth0" || network.SourceIP != "192.168.10.2" {
		t.Fatalf("détection non proposée: %#v", network)
	}
}

func TestReadinessRejectsEnabledMachineWithoutTransport(t *testing.T) {
	cfg := testConfig(t)
	cfg.Networks["lan"] = Network{CIDR: "127.0.0.0/8"}
	issues := readinessIssues(cfg)
	if len(issues) == 0 || !strings.Contains(issues[0], "aucun transport") {
		t.Fatalf("absence de transport non détectée: %v", issues)
	}
	cfg.Networks["lan"] = Network{CIDR: "127.0.0.0/8", AllowUnicast: true}
	if issues := readinessIssues(cfg); len(issues) != 0 {
		t.Fatalf("unicast utilisable refusé: %v", issues)
	}
}

func TestDetailedSummaryContainsOperationalSettings(t *testing.T) {
	cfg := testConfig(t)
	var output strings.Builder
	showConfigSummary(&output, cfg)
	for _, expected := range []string{"source=", "transports=", "destination=", "contrôles=", "logique=", "délais(initial=", "dépendances=", "active="} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("résumé incomplet (%s):\n%s", expected, output.String())
		}
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
