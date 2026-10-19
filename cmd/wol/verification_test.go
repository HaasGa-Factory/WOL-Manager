package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func durationPointer(value time.Duration) *Duration {
	duration := Duration(value)
	return &duration
}

func discardMagicPacket(string, string, bool, []byte) error { return nil }

func TestVerificationDefaultsAndOverrides(t *testing.T) {
	cfg := testConfig(t)
	initialDelay, timeout, interval := cfg.effectiveVerification("two")
	if initialDelay != 0 || timeout != 45*time.Second || interval != 2*time.Second {
		t.Fatalf("valeurs par défaut modifiées: %s, %s, %s", initialDelay, timeout, interval)
	}

	cfg.Global.Verification = VerificationConfig{
		InitialDelay: durationPointer(30 * time.Second),
		Timeout:      durationPointer(3 * time.Minute),
		Interval:     durationPointer(5 * time.Second),
	}
	server := cfg.Servers["two"]
	server.Verification.InitialDelay = durationPointer(90 * time.Second)
	cfg.Servers["two"] = server
	initialDelay, timeout, interval = cfg.effectiveVerification("two")
	if initialDelay != 90*time.Second || timeout != 3*time.Minute || interval != 5*time.Second {
		t.Fatalf("surcharge machine incorrecte: %s, %s, %s", initialDelay, timeout, interval)
	}

	override := 10 * time.Second
	engine := &Engine{cfg: cfg, verificationDelay: &override}
	initialDelay, _, _ = engine.verificationTiming("two")
	if initialDelay != override {
		t.Fatalf("surcharge CLI incorrecte: %s", initialDelay)
	}
}

func TestVerificationCLIOverrideAndInvalidValue(t *testing.T) {
	cfg := testConfig(t)
	engine := &Engine{cfg: cfg, audit: &Auditor{}, dry: true}
	if err := wakeCLI(cfg, engine, []string{"two", "--dry-run", "--verification-delay", "90s"}, true); err != nil {
		t.Fatal(err)
	}
	if engine.verificationDelay == nil || *engine.verificationDelay != 90*time.Second {
		t.Fatalf("option CLI non appliquée: %#v", engine.verificationDelay)
	}
	if err := wakeCLI(cfg, &Engine{cfg: cfg, audit: &Auditor{}}, []string{"two", "--verification-delay", "jamais"}, true); err == nil {
		t.Fatal("durée CLI invalide acceptée")
	}
}

func TestVerificationConfigRejectsInvalidDurations(t *testing.T) {
	for name, verification := range map[string]VerificationConfig{
		"initial négatif": {InitialDelay: durationPointer(-time.Second)},
		"timeout nul":     {Timeout: durationPointer(0)},
		"intervalle nul":  {Interval: durationPointer(0)},
		"délai excessif":  {InitialDelay: durationPointer(25 * time.Hour)},
		"intervalle supérieur au timeout": {
			Timeout:  durationPointer(time.Second),
			Interval: durationPointer(2 * time.Second),
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.Global.Verification = verification
			if err := cfg.Validate(); err == nil {
				t.Fatal("durée invalide acceptée")
			}
		})
	}
}

func TestWakeSkipsDelayWhenAlreadyReachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("socket local indisponible: %v", err)
	}
	defer listener.Close()
	cfg := verificationTestConfig(t, listener.Addr().(*net.TCPAddr).Port)
	server := cfg.Servers["two"]
	server.Verification.InitialDelay = durationPointer(2 * time.Second)
	cfg.Servers["two"] = server

	start := time.Now()
	result, err := (&Engine{cfg: cfg, audit: &Auditor{}, sendPacket: discardMagicPacket}).wakeTree(context.Background(), "two", map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Packet != "not_sent" || !result[0].Verified {
		t.Fatalf("machine joignable réveillée: %#v", result)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("le délai a été appliqué sans envoi de paquet")
	}
}

func TestWakeDryRunNeverWaits(t *testing.T) {
	cfg := testConfig(t)
	cfg.Global.Delay = Duration(2 * time.Second)
	server := cfg.Servers["two"]
	server.Verification.InitialDelay = durationPointer(2 * time.Second)
	cfg.Servers["two"] = server
	start := time.Now()
	result, err := (&Engine{cfg: cfg, audit: &Auditor{}, dry: true}).wakeTree(context.Background(), "two", map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Packet != "dry_run" {
		t.Fatalf("résultat dry-run inattendu: %#v", result)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("le dry-run a attendu le délai de vérification")
	}
}

type notificationWriter struct {
	once sync.Once
	done chan struct{}
	data bytes.Buffer
}

func (w *notificationWriter) Write(value []byte) (int, error) {
	written, err := w.data.Write(value)
	w.once.Do(func() { close(w.done) })
	return written, err
}

func TestWakeJobCancellationDuringInitialDelay(t *testing.T) {
	cfg := verificationTestConfig(t, unusedTCPPort(t))
	server := cfg.Servers["two"]
	server.Verification.InitialDelay = durationPointer(5 * time.Second)
	cfg.Servers["two"] = server
	progress := &notificationWriter{done: make(chan struct{})}
	engine := &Engine{cfg: cfg, audit: &Auditor{}, progress: progress, sendPacket: discardMagicPacket}
	store, err := openJobs(cfg.Storage.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.create("wake", "two", func(ctx context.Context) (any, error) {
		return engine.wakeTree(ctx, "two", map[string]bool{})
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-progress.done:
	case <-time.After(time.Second):
		t.Fatal("le job n'a pas atteint l'attente initiale")
	}
	if !strings.Contains(progress.data.String(), "Paquet envoyé, vérification dans 5 secondes…") {
		t.Fatalf("message d'attente absent: %q", progress.data.String())
	}
	if err := store.cancelJob(job.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, _ := store.get(job.ID)
		if current.State == "cancelled" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	current, _ := store.get(job.ID)
	t.Fatalf("job non annulé: %#v", current)
}

func TestWakeVerificationSucceedsAfterSeveralAttempts(t *testing.T) {
	port := unusedTCPPort(t)
	cfg := verificationTestConfig(t, port)
	server := cfg.Servers["two"]
	server.Verification.Timeout = durationPointer(500 * time.Millisecond)
	server.Verification.Interval = durationPointer(20 * time.Millisecond)
	cfg.Servers["two"] = server

	ready := make(chan net.Listener, 1)
	go func() {
		time.Sleep(80 * time.Millisecond)
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
		if err == nil {
			ready <- listener
		}
	}()
	result, err := (&Engine{cfg: cfg, audit: &Auditor{}, sendPacket: discardMagicPacket}).wakeTree(context.Background(), "two", map[string]bool{})
	select {
	case listener := <-ready:
		defer listener.Close()
	default:
	}
	if err != nil {
		if sandboxBlocksNetwork(err) {
			t.Skipf("réseau local bloqué: %v", err)
		}
		t.Fatal(err)
	}
	if len(result) != 1 || !result[0].Verified {
		t.Fatalf("machine non vérifiée après plusieurs tentatives: %#v", result)
	}
}

func TestWakeVerificationTimeoutReturnsFalse(t *testing.T) {
	cfg := verificationTestConfig(t, unusedTCPPort(t))
	server := cfg.Servers["two"]
	server.Verification.Timeout = durationPointer(80 * time.Millisecond)
	server.Verification.Interval = durationPointer(10 * time.Millisecond)
	cfg.Servers["two"] = server
	start := time.Now()
	result, err := (&Engine{cfg: cfg, audit: &Auditor{}, sendPacket: discardMagicPacket}).wakeTree(context.Background(), "two", map[string]bool{})
	if err != nil {
		if sandboxBlocksNetwork(err) {
			t.Skipf("réseau local bloqué: %v", err)
		}
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Verified {
		t.Fatalf("expiration incorrecte: %#v", result)
	}
	if time.Since(start) < 60*time.Millisecond {
		t.Fatal("la fenêtre de vérification n'a pas été respectée")
	}
}

func verificationTestConfig(t *testing.T, port int) *Config {
	t.Helper()
	cfg := testConfig(t)
	server := cfg.Servers["two"]
	server.IP = "127.0.0.1"
	server.Checks = []Check{{Type: "tcp", Port: port, Timeout: Duration(10 * time.Millisecond)}}
	cfg.Servers["two"] = server
	cfg.Global.WakeRetries = 0
	return cfg
}

func unusedTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("socket local indisponible: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}
