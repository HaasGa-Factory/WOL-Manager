package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func pipeNUTResponse(t *testing.T, response string, run func(*nutConnection) error) error {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		_, _ = bufio.NewReader(server).ReadString('\n')
		_, _ = server.Write([]byte(response))
	}()
	connection := &nutConnection{conn: client, rw: newNUTReadWriter(client)}
	err := run(connection)
	connection.shutdown()
	<-done
	return err
}

func TestNUTSingleLineLimits(t *testing.T) {
	for _, test := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "just_under", size: maxNUTLineBytes - 1},
		{name: "exact", size: maxNUTLineBytes},
		{name: "over", size: maxNUTLineBytes + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := strings.Repeat("A", test.size-1) + "\n"
			err := pipeNUTResponse(t, response, func(connection *nutConnection) error {
				_, err := connection.one("PING")
				return err
			})
			if test.wantErr != errors.Is(err, errNUTResponseLimit) {
				t.Fatalf("limite ligne size=%d: %v", test.size, err)
			}
		})
	}
}

func TestNUTListLimits(t *testing.T) {
	makeLines := func(count int, line string, withEnd bool) string {
		var response strings.Builder
		response.WriteString("BEGIN LIST UPS\n")
		for i := 1; i < count; i++ {
			if withEnd && i == count-1 {
				response.WriteString("END LIST UPS\n")
			} else {
				response.WriteString(line)
			}
		}
		return response.String()
	}
	runList := func(response string) error {
		return pipeNUTResponse(t, response, func(connection *nutConnection) error {
			_, err := connection.list("LIST UPS", "BEGIN LIST UPS", "END LIST UPS")
			return err
		})
	}
	if err := runList(makeLines(maxNUTListLines, "X\n", true)); err != nil {
		t.Fatalf("nombre maximal de lignes refusé: %v", err)
	}
	if err := runList(makeLines(maxNUTListLines+1, "X\n", true)); !errors.Is(err, errNUTResponseLimit) {
		t.Fatalf("ligne supplémentaire acceptée: %v", err)
	}
	largeLine := strings.Repeat("X", 8191) + "\n"
	lineCount := maxNUTResponseBytes/len(largeLine) + 2
	if err := runList(makeLines(lineCount, largeLine, true)); !errors.Is(err, errNUTResponseLimit) {
		t.Fatalf("taille cumulée excessive acceptée: %v", err)
	}
	if err := runList("BEGIN LIST UPS\nUPS main \"test\"\n"); err == nil || errors.Is(err, errNUTResponseLimit) {
		t.Fatalf("réponse sans END mal traitée: %v", err)
	}
}

func TestNUTErrorsNeverExposeCredentials(t *testing.T) {
	username := "private-user"
	password := "private-password"
	err := nutResponseError("ERR "+username+" "+password, nil)
	if strings.Contains(err.Error(), username) || strings.Contains(err.Error(), password) {
		t.Fatalf("identifiants exposés dans l'erreur: %v", err)
	}
}

func newRateTestServer(t *testing.T, now *time.Time, limit int) *APIServer {
	t.Helper()
	cfg := testConfig(t)
	cfg.API.RatePerMinute = limit
	return &APIServer{
		cfg:   cfg,
		rates: map[string]rateEntry{},
		now:   func() time.Time { return *now },
	}
}

func TestAPIRateLimiterWindowAndKeys(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	server := newRateTestServer(t, &now, 2)
	if !server.allowRequest("token-a|192.0.2.1") || !server.allowRequest("token-a|192.0.2.1") {
		t.Fatal("requêtes autorisées refusées dans la fenêtre")
	}
	if server.allowRequest("token-a|192.0.2.1") {
		t.Fatal("dépassement de limite accepté")
	}
	if !server.allowRequest("token-a|192.0.2.2") || !server.allowRequest("token-b|192.0.2.1") {
		t.Fatal("les couples jeton/IP ne sont pas indépendants")
	}
	now = now.Add(apiRateWindow)
	if !server.allowRequest("token-a|192.0.2.1") {
		t.Fatal("nouvelle fenêtre refusée")
	}
	server.mu.Lock()
	entries := len(server.rates)
	server.mu.Unlock()
	if entries != 1 {
		t.Fatalf("entrées expirées non supprimées: %d", entries)
	}
}

func TestAPIRateLimiterCardinalityPreservesActiveEntries(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	server := newRateTestServer(t, &now, 2)
	for i := 0; i < maxAPIRateEntries; i++ {
		if !server.allowRequest(fmt.Sprintf("token-%d|192.0.2.1", i)) {
			t.Fatalf("capacité atteinte trop tôt à %d", i)
		}
	}
	if server.allowRequest("new-token|192.0.2.1") {
		t.Fatal("nouvelle clé acceptée au-delà du plafond")
	}
	if !server.allowRequest("token-0|192.0.2.1") {
		t.Fatal("une clé active a été évincée pour contourner le plafond")
	}
	if server.allowRequest("token-0|192.0.2.1") {
		t.Fatal("une clé active a perdu son compteur lorsque la map était pleine")
	}
}

func TestAPIRateLimiterConcurrentCalls(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	server := newRateTestServer(t, &now, 50)
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if server.allowRequest("token|192.0.2.1") {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 50 {
		t.Fatalf("admissions concurrentes: %d, attendu 50", allowed.Load())
	}
}
