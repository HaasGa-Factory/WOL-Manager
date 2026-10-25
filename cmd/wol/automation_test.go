package main

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func automationConfig(t *testing.T) *Config {
	t.Helper()
	cfg := testConfig(t)
	low, high := 40, 70
	group := cfg.Groups["core"]
	group.Servers = []string{"two"}
	group.PowerRestore = &PowerRestorePolicy{
		Enabled:       true,
		UPS:           "ups-main",
		LowBattery:    &low,
		HighBattery:   &high,
		StableFor:     Duration(30 * time.Second),
		PollInterval:  Duration(5 * time.Second),
		Mode:          "sequential",
		FailurePolicy: "stop",
	}
	cfg.Groups["core"] = group
	cfg.UPS["ups-main"] = UPSConfig{Address: "127.0.0.1:3493", Name: "main"}
	cfg.Storage.StateFile = filepath.Join(testDir(t), "jobs.json")
	return cfg
}

func automationManagerForTest(t *testing.T, cfg *Config) (*AutomationManager, *JobStore) {
	t.Helper()
	jobs, err := openJobs(cfg.Storage.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	manager := newAutomationManager(cfg, jobs, &Engine{cfg: cfg, audit: &Auditor{}})
	return manager, jobs
}

func TestPowerRestoreDefaultsArmOnAnyObservedOutage(t *testing.T) {
	cfg := testConfig(t)
	group := cfg.Groups["core"]
	group.PowerRestore = &PowerRestorePolicy{Enabled: true, UPS: "ups-main"}
	cfg.Groups["core"] = group
	cfg.UPS["ups-main"] = UPSConfig{Address: "127.0.0.1:3493", Name: "main"}

	plans := cfg.restorePlans()
	if len(plans) != 1 {
		t.Fatalf("une politique attendue, obtenu %#v", plans)
	}
	if plans[0].LowBattery != 0 || plans[0].HighBattery != 70 {
		t.Fatalf("seuils par défaut = %d/%d, attendu 0/70", plans[0].LowBattery, plans[0].HighBattery)
	}
}

func TestPowerRestoreThresholdsAndStablePower(t *testing.T) {
	cfg := automationConfig(t)
	manager, jobs := automationManagerForTest(t, cfg)
	started := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	manager.handleStatus("ups-main", UPSStatus{State: "OB", Battery: 55, BatteryKnown: true}, started)
	cycle, ok := jobs.cycle("ups-main")
	if !ok || cycle.ArmedGroups["group.core"] {
		t.Fatalf("cycle non réarmé attendu au-dessus du seuil bas: %#v", cycle)
	}

	manager.handleStatus("ups-main", UPSStatus{State: "OL", Battery: 40, BatteryKnown: true}, started.Add(time.Second))
	cycle, _ = jobs.cycle("ups-main")
	if !cycle.ArmedGroups["group.core"] || cycle.StableSince == nil {
		t.Fatalf("retour secteur à 40%% non armé: %#v", cycle)
	}
	plans := manager.plansForUPS("ups-main")
	if ready := manager.readyPlans(cycle, plans, UPSStatus{State: "OL", Battery: 70, BatteryKnown: true}, started.Add(30*time.Second)); len(ready) != 0 {
		t.Fatal("politique libérée avant 30 secondes de stabilité")
	}
	if ready := manager.readyPlans(cycle, plans, UPSStatus{State: "OL", Battery: 69, BatteryKnown: true}, started.Add(31*time.Second)); len(ready) != 0 {
		t.Fatal("politique libérée sous le seuil haut")
	}
	if ready := manager.readyPlans(cycle, plans, UPSStatus{State: "OL", Battery: 70, BatteryKnown: true}, started.Add(31*time.Second)); len(ready) != 1 {
		t.Fatalf("politique non libérée à 70%% après stabilité: %#v", ready)
	}
}

func TestPowerRestoreRequiresRecordedOutageAndKnownBattery(t *testing.T) {
	cfg := automationConfig(t)
	manager, jobs := automationManagerForTest(t, cfg)
	now := time.Now().UTC()
	manager.handleStatus("ups-main", UPSStatus{State: "OL", Battery: 100, BatteryKnown: true}, now)
	if cycles := jobs.listCycles(); len(cycles) != 0 {
		t.Fatalf("démarrage automatique sans cycle enregistré: %#v", cycles)
	}
	stable := now.Add(-time.Minute)
	cycle := PowerCycle{UPS: "ups-main", State: "waiting", StableSince: &stable, ArmedGroups: map[string]bool{"group.core": true}, CompletedGroups: map[string]bool{}}
	if ready := manager.readyPlans(cycle, manager.plansForUPS("ups-main"), UPSStatus{State: "OL"}, now); len(ready) != 0 {
		t.Fatal("batterie absente interprétée comme une mesure fiable")
	}
}

func TestUPSSafetyBlockReturnsAutomaticCycleToWaiting(t *testing.T) {
	cfg := automationConfig(t)
	manager, jobs := automationManagerForTest(t, cfg)
	now := time.Now().UTC()
	stable := now.Add(-time.Minute)
	cycle := PowerCycle{
		UPS:             "ups-main",
		State:           "running",
		StableSince:     &stable,
		ArmedGroups:     map[string]bool{"group.core": true},
		CompletedGroups: map[string]bool{},
	}
	if err := jobs.setCycle(cycle); err != nil {
		t.Fatal(err)
	}
	plans := manager.plansForUPS("ups-main")
	err := fmt.Errorf("groupe core: %w", upsRevalidationErrorf("politique UPS: batterie 60%% < 70%%"))
	if err := manager.finishCycle("ups-main", plans, err); err != nil {
		t.Fatal(err)
	}

	stored, _ := jobs.cycle("ups-main")
	if stored.State != "waiting" || stored.StableSince != nil {
		t.Fatalf("blocage UPS devenu terminal: %#v", stored)
	}
	if !strings.Contains(stored.Reason, "nouvelle vérification NUT") {
		t.Fatalf("raison de revalidation absente: %q", stored.Reason)
	}
	if err := manager.gate.AllowUPS("ups-main"); !needsUPSRevalidation(err) {
		t.Fatalf("verrou UPS non conservé: %v", err)
	}

	manager.handleStatus("ups-main", UPSStatus{State: "OL", Battery: 70, BatteryKnown: true}, now)
	stored, _ = jobs.cycle("ups-main")
	if stored.State != "waiting" || stored.StableSince == nil {
		t.Fatalf("cycle non remis en surveillance: %#v", stored)
	}
	if ready := manager.readyPlans(stored, plans, UPSStatus{State: "OL", Battery: 70, BatteryKnown: true}, now.Add(31*time.Second)); len(ready) != 1 {
		t.Fatalf("cycle non reprenable après revalidation: %#v", ready)
	}
}

func TestPermanentRestoreErrorRemainsFailed(t *testing.T) {
	cfg := automationConfig(t)
	manager, jobs := automationManagerForTest(t, cfg)
	if err := jobs.setCycle(PowerCycle{
		UPS:             "ups-main",
		State:           "running",
		ArmedGroups:     map[string]bool{"group.core": true},
		CompletedGroups: map[string]bool{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.finishCycle("ups-main", manager.plansForUPS("ups-main"), fmt.Errorf("configuration invalide")); err != nil {
		t.Fatal(err)
	}
	stored, _ := jobs.cycle("ups-main")
	if stored.State != "failed" {
		t.Fatalf("erreur permanente rendue récupérable: %#v", stored)
	}
}

func TestCompletedPlanSurvivesUPSSafetyRevalidation(t *testing.T) {
	cfg := automationConfig(t)
	manager, jobs := automationManagerForTest(t, cfg)
	if err := jobs.setCycle(PowerCycle{UPS: "ups-main", State: "running", ArmedGroups: map[string]bool{"group.core": true}}); err != nil {
		t.Fatal(err)
	}
	if err := manager.completePlan("ups-main", "group.core"); err != nil {
		t.Fatal(err)
	}
	if err := manager.finishCycle("ups-main", manager.plansForUPS("ups-main"), upsRevalidationErrorf("UPS temporairement indisponible")); err != nil {
		t.Fatal(err)
	}
	stored, _ := jobs.cycle("ups-main")
	if !stored.CompletedGroups["group.core"] || stored.State != "waiting" {
		t.Fatalf("progression perdue pendant la revalidation: %#v", stored)
	}
}

func TestCompletedAndFailedCyclesDoNotRestartOnOnlinePoll(t *testing.T) {
	for _, state := range []string{"completed", "failed"} {
		t.Run(state, func(t *testing.T) {
			cfg := automationConfig(t)
			manager, jobs := automationManagerForTest(t, cfg)
			cycle := PowerCycle{UPS: "ups-main", State: state, Reason: "résultat conservé", ArmedGroups: map[string]bool{"group.core": true}, CompletedGroups: map[string]bool{"group.core": state == "completed"}}
			if err := jobs.setCycle(cycle); err != nil {
				t.Fatal(err)
			}
			manager.handleStatus("ups-main", UPSStatus{State: "unknown", Error: "NUT indisponible"}, time.Now().UTC())
			manager.handleStatus("ups-main", UPSStatus{State: "OL", Battery: 100, BatteryKnown: true}, time.Now().UTC())
			stored, _ := jobs.cycle("ups-main")
			if stored.State != state || jobs.activeJob("automation_restore", "ups-main") {
				t.Fatalf("cycle %s redémarré: %#v", state, stored)
			}
			_ = newAutomationManager(cfg, jobs, &Engine{cfg: cfg, audit: &Auditor{}})
			stored, _ = jobs.cycle("ups-main")
			if stored.State != state {
				t.Fatalf("cycle terminal %s réarmé au redémarrage: %#v", state, stored)
			}
		})
	}
}

func TestPowerRestoreUnsafeStatesBlockAndCancel(t *testing.T) {
	for _, state := range []string{"OB", "LB", "FSD", "unknown"} {
		t.Run(state, func(t *testing.T) {
			cfg := automationConfig(t)
			manager, jobs := automationManagerForTest(t, cfg)
			started := make(chan struct{})
			job, err := jobs.create("automation_restore", "ups-main", func(ctx context.Context) (any, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			})
			if err != nil {
				t.Fatal(err)
			}
			<-started
			manager.handleStatus("ups-main", UPSStatus{State: state}, time.Now().UTC())
			if err := manager.gate.AllowUPS("ups-main"); err == nil {
				t.Fatalf("état %s non bloqué", state)
			}
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				current, _ := jobs.get(job.ID)
				if current.State == "cancelled" {
					return
				}
				time.Sleep(time.Millisecond)
			}
			t.Fatalf("tâche non annulée pour état %s", state)
		})
	}
}

func TestNewOutageRevalidatesPreviouslyCompletedGroups(t *testing.T) {
	cfg := automationConfig(t)
	manager, jobs := automationManagerForTest(t, cfg)
	old := time.Now().UTC().Add(-time.Minute)
	if err := jobs.setCycle(PowerCycle{
		UPS:             "ups-main",
		State:           "running",
		ObservedAt:      old,
		ArmedGroups:     map[string]bool{"group.core": true},
		CompletedGroups: map[string]bool{"group.core": true},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	manager.handleStatus("ups-main", UPSStatus{State: "OB", Battery: 30, BatteryKnown: true}, now)
	cycle, _ := jobs.cycle("ups-main")
	if cycle.CompletedGroups["group.core"] || !cycle.ArmedGroups["group.core"] || !cycle.ObservedAt.Equal(now) {
		t.Fatalf("nouvelle coupure non recalculée: %#v", cycle)
	}
}

func TestPowerCyclePersistenceBlocksUntilRevalidation(t *testing.T) {
	cfg := automationConfig(t)
	_, jobs := automationManagerForTest(t, cfg)
	if err := jobs.setCycle(PowerCycle{UPS: "ups-main", State: "running", ArmedGroups: map[string]bool{"group.core": true}}); err != nil {
		t.Fatal(err)
	}
	reopened, err := openJobs(cfg.Storage.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	manager := newAutomationManager(cfg, reopened, &Engine{cfg: cfg, audit: &Auditor{}})
	cycle, _ := reopened.cycle("ups-main")
	if cycle.State != "waiting" || cycle.StableSince != nil {
		t.Fatalf("cycle non remis en revalidation: %#v", cycle)
	}
	if err := manager.gate.AllowUPS("ups-main"); err == nil {
		t.Fatal("réveil autorisé avant revalidation NUT")
	}
}

func TestCheckLogicAnyAndAllWithMultipleTCPPorts(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	openPort := listener.Addr().(*net.TCPAddr).Port
	closedListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closedListener.Addr().(*net.TCPAddr).Port
	closedListener.Close()

	cfg := testConfig(t)
	server := cfg.Servers["two"]
	server.IP = "127.0.0.1"
	server.Checks = []Check{{Type: "tcp", Port: openPort}, {Type: "tcp", Port: closedPort}}
	server.CheckLogic = "any"
	cfg.Servers["two"] = server
	if result := checkServer(context.Background(), cfg, "two"); result.State != "reachable" {
		t.Fatalf("logique OU = %s", result.State)
	}
	server.CheckLogic = "all"
	cfg.Servers["two"] = server
	if result := checkServer(context.Background(), cfg, "two"); result.State != "unreachable" {
		t.Fatalf("logique ET = %s", result.State)
	}
	server.CheckLogic = "any"
	server.Checks = []Check{{Type: "tcp", Port: closedPort}, {Type: "arp"}}
	cfg.Servers["two"] = server
	if result := checkServer(context.Background(), cfg, "two"); result.State != "unknown" {
		t.Fatalf("logique OU avec contrôle indéterminé = %s", result.State)
	}
}

func TestRestorePlansSkipDisabledPolicyAndKeepDeterministicOrder(t *testing.T) {
	cfg := automationConfig(t)
	low, high := 20, 80
	cfg.Groups["later"] = Group{Priority: 20, Servers: []string{"two"}, PowerRestore: &PowerRestorePolicy{Enabled: true, UPS: "ups-main", LowBattery: &low, HighBattery: &high}}
	disabled := cfg.Groups["core"]
	disabled.PowerRestore.Enabled = false
	cfg.Groups["core"] = disabled
	plans := cfg.restorePlans()
	if len(plans) != 1 || plans[0].Group != "later" {
		t.Fatalf("politiques actives incorrectes: %#v", plans)
	}
	order := cfg.orderedGroupMembers("later", []string{"two"})
	if len(order) != 1 || order[0] != "two" {
		t.Fatalf("ordre non déterministe: %v", order)
	}
}

func TestWakeCoordinatorRetriesOnlyAfterExplicitReset(t *testing.T) {
	coordinator := newWakeCoordinator()
	owner, err := coordinator.acquire(context.Background(), "server")
	if err != nil || !owner {
		t.Fatalf("premier propriétaire refusé: owner=%v err=%v", owner, err)
	}
	failed := context.DeadlineExceeded
	coordinator.complete("server", failed)
	owner, err = coordinator.acquire(context.Background(), "server")
	if owner || err != failed {
		t.Fatalf("échec partagé transformé en retry implicite: owner=%v err=%v", owner, err)
	}
	coordinator.resetFailed("server")
	owner, err = coordinator.acquire(context.Background(), "server")
	if err != nil || !owner {
		t.Fatalf("retry explicite refusé: owner=%v err=%v", owner, err)
	}
}

func TestSequentialAndParallelRestoreModes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	cfg := testConfig(t)
	cfg.Servers = map[string]Server{
		"first":  {Name: "First", MAC: "02:00:00:00:00:11", IP: "127.0.0.1", Network: "lan", Checks: []Check{{Type: "tcp", Port: port}}},
		"second": {Name: "Second", MAC: "02:00:00:00:00:12", IP: "127.0.0.1", Network: "lan", Checks: []Check{{Type: "tcp", Port: port}}},
	}
	cfg.Groups = map[string]Group{"restore": {Priority: 10, Servers: []string{"second", "first"}}}
	engine := &Engine{cfg: cfg, audit: &Auditor{}, requireVerified: true, coordinator: newWakeCoordinator()}
	sequential := restorePlan{Group: "restore", Mode: "sequential", Order: []string{"first", "second"}, FailurePolicy: "stop"}
	results, err := engine.wakeRestoreGroup(context.Background(), sequential)
	if err != nil || len(results) != 2 || results[0].Server != "first" || results[1].Server != "second" {
		t.Fatalf("mode séquentiel incorrect: results=%#v err=%v", results, err)
	}
	engine.coordinator = newWakeCoordinator()
	parallel := sequential
	parallel.Mode = "parallel"
	results, err = engine.wakeRestoreGroup(context.Background(), parallel)
	if err != nil || len(results) != 2 {
		t.Fatalf("mode parallèle incorrect: results=%#v err=%v", results, err)
	}
	for _, result := range results {
		if result.Packet != "not_sent" || !result.Verified {
			t.Fatalf("machine déjà joignable réveillée inutilement: %#v", result)
		}
	}
}

func TestCommonServerAcrossEligibleGroupsRunsOnce(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	cfg := testConfig(t)
	server := cfg.Servers["two"]
	server.IP = "127.0.0.1"
	server.Checks = []Check{{Type: "tcp", Port: port}}
	cfg.Servers["two"] = server
	cfg.Groups = map[string]Group{
		"first":  {Priority: 10, Servers: []string{"two"}},
		"second": {Priority: 20, Servers: []string{"two"}},
	}
	manager, jobs := automationManagerForTest(t, cfg)
	if err := jobs.setCycle(PowerCycle{
		UPS:             "ups-main",
		State:           "waiting",
		ArmedGroups:     map[string]bool{"first": true, "second": true},
		CompletedGroups: map[string]bool{},
	}); err != nil {
		t.Fatal(err)
	}
	plans := []restorePlan{
		{ID: "first", Group: "first", UPS: "ups-main", Mode: "sequential", FailurePolicy: "stop"},
		{ID: "second", Group: "second", UPS: "ups-main", Mode: "sequential", FailurePolicy: "stop"},
	}
	results, err := manager.runPlans(context.Background(), "ups-main", plans)
	if err != nil || len(results) != 1 || results[0].Server != "two" {
		t.Fatalf("serveur commun exécuté plusieurs fois: results=%#v err=%v", results, err)
	}
}

func TestPowerRestoreValidationAndV111Compatibility(t *testing.T) {
	cfg := automationConfig(t)
	group := cfg.Groups["core"]
	group.PowerRestore.HighBattery = intPointer(40)
	group.PowerRestore.RetryLimit = 1
	group.PowerRestore.RetryInterval = 0
	group.PowerRestore.FailurePolicy = "retry"
	cfg.Groups["core"] = group
	if err := cfg.Validate(); err == nil {
		t.Fatal("seuils identiques et intervalle nul acceptés")
	}

	legacy := []byte(`version: 1
global:
  port: 9
  transport_order: [unicast]
networks:
  lan:
    cidr: 127.0.0.0/8
    allow_unicast: true
groups:
  core:
    priority: 10
    servers: [two]
servers:
  two:
    name: Two
    mac: "02:00:00:00:00:02"
    ip: 127.0.0.2
    network: lan
api:
  listen: 127.0.0.1:8080
storage:
  state_file: /tmp/wol-state.json
  audit_file: /tmp/wol-audit.jsonl
network_discovery:
  allowed_subnets: [127.0.0.0/8]
  safeguards:
    max_packets: 2
    rate_limit_pps: 1
    timeout: 1s
ups: {}
relays: {}
`)
	parsed, err := loadConfigData(legacy)
	if err != nil {
		t.Fatalf("configuration V1.1.1 illisible: %v", err)
	}
	if err := parsed.Validate(); err != nil {
		t.Fatalf("configuration V1.1.1 incompatible: %v", err)
	}
}
