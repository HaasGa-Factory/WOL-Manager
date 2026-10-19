package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type Engine struct {
	cfg               *Config
	audit             *Auditor
	dry               bool
	noWait            bool
	verificationDelay *time.Duration
	progress          io.Writer
	gate              *PowerGate
	requireVerified   bool
	automationUPS     string
	coordinator       *wakeCoordinator
	sendPacket        func(destination, source string, broadcast bool, packet []byte) error
}
type WakeResult struct {
	Server      string        `json:"server"`
	Transport   string        `json:"transport"`
	Destination string        `json:"destination,omitempty"`
	Packet      string        `json:"packet"`
	Verified    bool          `json:"verified"`
	Status      *StatusResult `json:"status,omitempty"`
	Note        string        `json:"note"`
}

func (e *Engine) wakeTree(ctx context.Context, id string, done map[string]bool) ([]WakeResult, error) {
	if e.coordinator != nil {
		owner, err := e.coordinator.acquire(ctx, id)
		if !owner {
			return nil, err
		}
		result, runErr := e.wakeTreeCore(ctx, id, done)
		e.coordinator.complete(id, runErr)
		return result, runErr
	}
	return e.wakeTreeCore(ctx, id, done)
}

func (e *Engine) wakeTreeCore(ctx context.Context, id string, done map[string]bool) ([]WakeResult, error) {
	if done[id] {
		return nil, nil
	}
	s, ok := e.cfg.Servers[id]
	if !ok {
		return nil, fmt.Errorf("serveur inconnu %q", id)
	}
	if !enabled(s) {
		return nil, fmt.Errorf("serveur %s désactivé", id)
	}
	if !e.dry {
		status := checkServer(ctx, e.cfg, id)
		if status.State == "reachable" {
			done[id] = true
			return []WakeResult{{Server: id, Packet: "not_sent", Verified: true, Status: &status, Note: "machine déjà joignable; aucun paquet émis"}}, nil
		}
	}
	var out []WakeResult
	for _, d := range e.cfg.dependencies(id) {
		x, err := e.wakeTree(ctx, d, done)
		out = append(out, x...)
		if err != nil {
			return out, fmt.Errorf("dépendance %s: %w", d, err)
		}
	}
	if !e.dry {
		if err := checkUPSPolicy(ctx, e.cfg, id); err != nil {
			return out, err
		}
	}
	x, err := e.wakeOne(ctx, id)
	out = append(out, x)
	if err != nil {
		return out, err
	}
	if e.requireVerified && !x.Verified {
		return out, fmt.Errorf("serveur %s non validé après le réveil", id)
	}
	done[id] = true
	_, _, delay := e.cfg.effectiveTiming(id)
	if !e.dry && delay > 0 {
		if err := waitContext(ctx, delay); err != nil {
			return out, err
		}
	}
	return out, nil
}
func (e *Engine) wakeOne(ctx context.Context, id string) (WakeResult, error) {
	plan, err := transportPlan(e.cfg, id)
	if err != nil {
		return WakeResult{Server: id, Packet: "not_sent", Note: err.Error()}, err
	}
	var d TransportDecision
	for _, x := range plan {
		if x.State == "selected" {
			d = x
			break
		}
	}
	r := WakeResult{Server: id, Transport: d.Name, Destination: d.Destination, Packet: "not_sent", Note: "un paquet émis ne garantit pas le réveil"}
	if e.dry {
		r.Packet = "dry_run"
		r.Note = "simulation: aucun paquet émis; disponibilité et politique UPS à revalider lors de l'exécution"
		e.audit.write("wake", "cli", id, "dry_run", d)
		return r, nil
	}
	mac, _ := net.ParseMAC(e.cfg.Servers[id].MAC)
	packet := magicPacket(mac)
	initialDelay, timeout, interval := e.verificationTiming(id)
	maxChecks, packetRetries := e.cfg.effectiveVerificationAttempts(id)
	packetAttempts := packetRetries + 1
	checks := 0
	var verificationContext context.Context
	var cancelVerification context.CancelFunc
	var deadline time.Time
	verificationStarted := false
attemptsLoop:
	for attempt := 1; attempt <= packetAttempts; attempt++ {
		if e.gate != nil {
			var gateErr error
			if e.automationUPS != "" {
				gateErr = e.gate.AllowUPS(e.automationUPS)
			} else {
				gateErr = e.gate.AllowServer(e.cfg, id)
			}
			if gateErr != nil {
				return r, gateErr
			}
		}
		if d.Name == "relay" {
			err = e.sendRelay(ctx, d.Relay, id)
		} else {
			n := e.cfg.Networks[e.cfg.Servers[id].Network]
			if e.sendPacket == nil {
				err = fmt.Errorf("émetteur Magic Packet non configuré")
			} else {
				err = e.sendPacket(d.Destination, sourceForNetwork(n), d.Name != "unicast", packet)
			}
		}
		if err != nil {
			if attempt == packetAttempts {
				e.audit.write("wake", "engine", id, "failed", err.Error())
				return r, err
			}
			continue
		}
		r.Packet = "sent"
		if !verificationStarted {
			if initialDelay > 0 {
				if e.progress != nil {
					infof(e.progress, "Paquet envoyé, vérification dans %s…", frenchDuration(initialDelay))
				}
				if err := waitContext(ctx, initialDelay); err != nil {
					return r, err
				}
			}
			deadline = time.Now().Add(timeout)
			verificationContext, cancelVerification = context.WithDeadline(ctx, deadline)
			defer cancelVerification()
			verificationStarted = true
		}
		window := deadline
		if remaining := packetAttempts - attempt + 1; remaining > 1 {
			window = time.Now().Add(time.Until(deadline) / time.Duration(remaining))
		}
		for {
			status := checkServer(verificationContext, e.cfg, id)
			checks++
			r.Status = &status
			r.Verified = status.State == "reachable"
			if r.Verified {
				r.Note = "paquet émis et machine joignable selon les vérifications configurées"
				e.audit.write("wake", "engine", id, "verified", r)
				return r, nil
			}
			if e.noWait {
				e.audit.write("wake", "engine", id, "sent", r)
				return r, nil
			}
			if maxChecks > 0 && checks >= maxChecks {
				break attemptsLoop
			}
			if ctx.Err() != nil {
				return r, ctx.Err()
			}
			if verificationContext.Err() != nil {
				break attemptsLoop
			}
			remaining := time.Until(window)
			if remaining <= 0 {
				break
			}
			pause := interval
			if pause > remaining {
				pause = remaining
			}
			if err := waitContext(verificationContext, pause); err != nil {
				if ctx.Err() != nil {
					return r, ctx.Err()
				}
				break attemptsLoop
			}
			if !time.Now().Before(window) {
				break
			}
		}
	}
	e.audit.write("wake", "engine", id, "sent", r)
	return r, nil
}

func (e *Engine) verificationTiming(id string) (initialDelay, timeout, interval time.Duration) {
	initialDelay, timeout, interval = e.cfg.effectiveVerification(id)
	if e.verificationDelay != nil {
		initialDelay = *e.verificationDelay
	}
	return initialDelay, timeout, interval
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func frenchDuration(delay time.Duration) string {
	if delay%time.Second == 0 {
		seconds := int64(delay / time.Second)
		if seconds == 1 {
			return "1 seconde"
		}
		return fmt.Sprintf("%d secondes", seconds)
	}
	return delay.String()
}
func (e *Engine) sendRelay(ctx context.Context, relay, id string) error {
	r := e.cfg.Relays[relay]
	cl, err := relayHTTPClient(r)
	if err != nil {
		return err
	}
	requestID, err := newID()
	if err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]string{"server": id, "request_id": requestID})
	req, _ := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(r.URL, "/")+"/relay/v1/wake", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("relais: %s: %s", resp.Status, string(msg))
	}
	return nil
}

func relayHTTPClient(r RelayConfig) (*http.Client, error) {
	pool := x509.NewCertPool()
	ca, err := os.ReadFile(r.CACert)
	if err != nil {
		return nil, err
	}
	if !pool.AppendCertsFromPEM(ca) {
		return nil, fmt.Errorf("CA relais invalide")
	}
	cert, err := tls.LoadX509KeyPair(r.ClientCert, r.ClientKey)
	if err != nil {
		return nil, err
	}
	return &http.Client{Timeout: r.Timeout.Value(5 * time.Second), Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool, Certificates: []tls.Certificate{cert}}}}, nil
}

func relayHealth(ctx context.Context, r RelayConfig) error {
	cl, err := relayHTTPClient(r)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(r.URL, "/")+"/relay/v1/health", nil)
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relais: %s", resp.Status)
	}
	return nil
}
