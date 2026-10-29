package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type TokenRecord struct {
	ID          string   `json:"id"`
	Hash        string   `json:"sha256"`
	Permissions []string `json:"permissions"`
	Disabled    bool     `json:"disabled,omitempty"`
}
type tokenFile struct {
	Tokens []TokenRecord `json:"tokens"`
}
type principal struct {
	ID          string
	permissions map[string]bool
}
type ctxKey int

const principalKey ctxKey = 1

const (
	apiRateWindow     = time.Minute
	maxAPIRateEntries = 4096
)

type rateEntry struct {
	start time.Time
	count int
}
type APIServer struct {
	cfg         *Config
	jobs        *JobStore
	engine      *Engine
	tokens      []TokenRecord
	tokenPath   string
	mu          sync.Mutex
	rates       map[string]rateEntry
	now         func() time.Time
	automations *AutomationManager
}

func loadTokens(path string) ([]TokenRecord, error) {
	if info, err := os.Stat(path); err == nil && runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("permissions dangereuses sur %s: utiliser 0600", path)
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var f tokenFile
	if e = json.Unmarshal(b, &f); e != nil {
		return nil, e
	}
	if len(f.Tokens) == 0 {
		return nil, errors.New("aucun jeton configuré")
	}
	for _, t := range f.Tokens {
		b, e := hex.DecodeString(t.Hash)
		if e != nil || len(b) != 32 {
			return nil, fmt.Errorf("hash invalide pour le jeton %s", t.ID)
		}
	}
	return f.Tokens, nil
}
func (s *APIServer) authenticate(r *http.Request) (principal, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return principal{}, false
	}
	sum := sha256.Sum256([]byte(strings.TrimPrefix(h, "Bearer ")))
	tokens := s.tokens
	if s.tokenPath != "" {
		var err error
		tokens, err = loadTokens(s.tokenPath)
		if err != nil {
			return principal{}, false
		}
	}
	for _, t := range tokens {
		expected, _ := hex.DecodeString(t.Hash)
		if !t.Disabled && subtle.ConstantTimeCompare(sum[:], expected) == 1 {
			p := principal{ID: t.ID, permissions: map[string]bool{}}
			for _, x := range t.Permissions {
				p.permissions[x] = true
			}
			return p, true
		}
	}
	return principal{}, false
}
func (s *APIServer) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		p, ok := s.authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "jeton absent ou invalide")
			return
		}
		key := p.ID + "|" + clientIP(r)
		if !s.allowRequest(key) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "limite de requêtes atteinte")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
	})
}

func (s *APIServer) allowRequest(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	if s.rates == nil {
		s.rates = make(map[string]rateEntry)
	}
	for existingKey, entry := range s.rates {
		if now.Sub(entry.start) >= apiRateWindow {
			delete(s.rates, existingKey)
		}
	}
	entry, exists := s.rates[key]
	if !exists {
		if len(s.rates) >= maxAPIRateEntries {
			return false
		}
		entry.start = now
	}
	entry.count++
	s.rates[key] = entry
	return entry.count <= s.cfg.API.RatePerMinute
}
func need(w http.ResponseWriter, r *http.Request, permission string) bool {
	p, _ := r.Context().Value(principalKey).(principal)
	if p.permissions["admin"] || p.permissions[permission] {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden", "permission "+permission+" requise")
	return false
}
func clientIP(r *http.Request) string {
	h, _, e := net.SplitHostPort(r.RemoteAddr)
	if e == nil {
		return h
	}
	return r.RemoteAddr
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", e.Error())
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json", "un seul objet JSON est accepté")
		return false
	}
	return true
}

func (s *APIServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.health)
	mux.HandleFunc("/api/v1/servers", s.servers)
	mux.HandleFunc("/api/v1/servers/", s.server)
	mux.HandleFunc("/api/v1/groups/", s.group)
	mux.HandleFunc("/api/v1/wake-all", s.wakeAll)
	mux.HandleFunc("/api/v1/ups", s.ups)
	mux.HandleFunc("/api/v1/automations", s.automationState)
	mux.HandleFunc("/api/v1/jobs/", s.job)
	mux.HandleFunc("/api/v1/networks", s.networks)
	mux.HandleFunc("/api/v1/networks/diagnostics", s.diagnostics)
	mux.HandleFunc("/api/v1/networks/discovery", s.discovery)
	return s.middleware(mux)
}
func (s *APIServer) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		method(w)
		return
	}
	if !need(w, r, "read") {
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok", "version": version})
}
func (s *APIServer) servers(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		method(w)
		return
	}
	if !need(w, r, "read") {
		return
	}
	writeJSON(w, 200, s.cfg.Servers)
}
func (s *APIServer) server(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/servers/"), "/")
	if len(parts) != 2 {
		writeError(w, 404, "not_found", "ressource inconnue")
		return
	}
	id, action := parts[0], parts[1]
	if _, ok := s.cfg.Servers[id]; !ok {
		writeError(w, 404, "not_found", "serveur inconnu")
		return
	}
	switch {
	case r.Method == "GET" && action == "status":
		if !need(w, r, "read") {
			return
		}
		writeJSON(w, 200, checkServer(r.Context(), s.cfg, id))
	case r.Method == "POST" && action == "wake":
		if !need(w, r, "wake") {
			return
		}
		var body struct {
			DryRun bool `json:"dry_run"`
		}
		if r.ContentLength != 0 && !decodeJSON(w, r, &body) {
			return
		}
		j, e := s.jobs.create("wake", id, func(ctx context.Context) (any, error) {
			engine := *s.engine
			engine.dry = body.DryRun
			return engine.wakeTree(ctx, id, map[string]bool{})
		})
		if e != nil {
			writeError(w, 409, "job_conflict", e.Error())
			return
		}
		s.engine.audit.write("api.wake", principalOf(r), id, "accepted", map[string]any{"job": j.ID, "dry_run": body.DryRun})
		writeJSON(w, 202, j)
	default:
		method(w)
	}
}
func (s *APIServer) group(w http.ResponseWriter, r *http.Request) {
	p := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/groups/"), "/")
	if r.Method != "POST" || len(p) != 2 || p[1] != "wake" {
		method(w)
		return
	}
	if !need(w, r, "wake") {
		return
	}
	if _, ok := s.cfg.Groups[p[0]]; !ok {
		writeError(w, 404, "not_found", "groupe inconnu")
		return
	}
	s.startGroup(w, r, p[0])
}
func (s *APIServer) startGroup(w http.ResponseWriter, r *http.Request, id string) {
	j, e := s.jobs.create("wake_group", id, func(ctx context.Context) (any, error) { return s.engine.wakeGroup(ctx, id) })
	if e != nil {
		writeError(w, 409, "job_conflict", e.Error())
		return
	}
	writeJSON(w, 202, j)
}
func (s *APIServer) wakeAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		method(w)
		return
	}
	if !need(w, r, "wake") {
		return
	}
	j, e := s.jobs.create("wake_all", "all", func(ctx context.Context) (any, error) { return s.engine.wakeAll(ctx) })
	if e != nil {
		writeError(w, 409, "job_conflict", e.Error())
		return
	}
	writeJSON(w, 202, j)
}
func (s *APIServer) ups(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		method(w)
		return
	}
	if !need(w, r, "read") {
		return
	}
	out := map[string]UPSStatus{}
	for id, u := range s.cfg.UPS {
		out[id] = nutStatus(r.Context(), id, u)
	}
	writeJSON(w, 200, out)
}

func (s *APIServer) automationState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		method(w)
		return
	}
	if !need(w, r, "read") {
		return
	}
	if s.automations == nil {
		writeJSON(w, http.StatusOK, map[string]any{"cycles": []PowerCycle{}, "policies": []restorePlan{}})
		return
	}
	writeJSON(w, http.StatusOK, s.automations.automationStatus())
}
func (s *APIServer) job(w http.ResponseWriter, r *http.Request) {
	p := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/jobs/"), "/")
	if len(p) < 1 || p[0] == "" {
		writeError(w, 404, "not_found", "tâche inconnue")
		return
	}
	if r.Method == "GET" && len(p) == 1 {
		if !need(w, r, "read") {
			return
		}
		j, ok := s.jobs.get(p[0])
		if !ok {
			writeError(w, 404, "not_found", "tâche inconnue")
			return
		}
		writeJSON(w, 200, j)
		return
	}
	if r.Method == "POST" && len(p) == 2 && p[1] == "cancel" {
		if !need(w, r, "admin") {
			return
		}
		if e := s.jobs.cancelJob(p[0]); e != nil {
			writeError(w, 409, "not_cancellable", e.Error())
			return
		}
		writeJSON(w, 202, map[string]string{"status": "cancellation_requested"})
		return
	}
	method(w)
}
func (s *APIServer) networks(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		method(w)
		return
	}
	if !need(w, r, "read") {
		return
	}
	writeJSON(w, 200, s.cfg.Networks)
}
func (s *APIServer) diagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		method(w)
		return
	}
	if !need(w, r, "read") {
		return
	}
	out := map[string]any{}
	for id := range s.cfg.Servers {
		plan, e := transportPlan(s.cfg, id)
		out[id] = map[string]any{"plan": plan, "error": errorString(e)}
	}
	writeJSON(w, 200, out)
}
func (s *APIServer) discovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		method(w)
		return
	}
	if !need(w, r, "admin") {
		return
	}
	var b struct {
		Active  bool `json:"active"`
		Confirm bool `json:"confirm"`
		DryRun  bool `json:"dry_run"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	if !b.Active && !s.cfg.Discovery.PassiveEnabled {
		writeError(w, 403, "passive_discovery_blocked", "découverte passive désactivée")
		return
	}
	if b.Active && (!s.cfg.API.ActiveDiscovery || !s.cfg.Discovery.ActiveEnabled || !b.Confirm) {
		writeError(w, 403, "active_discovery_blocked", "activation serveur et confirmation explicite requises")
		return
	}
	j, e := s.jobs.create("network_discovery", fmt.Sprint(b.Active), func(ctx context.Context) (any, error) {
		if !b.Active {
			return passiveDiscovery(ctx), nil
		}
		timeout := s.cfg.Discovery.Safeguards.Timeout.Value(30 * time.Second)
		cc, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return activeDiscovery(cc, s.cfg, b.DryRun), nil
	})
	if e != nil {
		writeError(w, 409, "job_conflict", e.Error())
		return
	}
	s.engine.audit.write("api.network.discovery", principalOf(r), "configured_networks", "accepted", map[string]any{"job": j.ID, "active": b.Active, "dry_run": b.DryRun})
	writeJSON(w, 202, j)
}
func principalOf(r *http.Request) string {
	p, _ := r.Context().Value(principalKey).(principal)
	return p.ID
}
func method(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "méthode non autorisée")
}
func errorString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

func (e *Engine) wakeGroup(ctx context.Context, id string) ([]WakeResult, error) {
	g, ok := e.cfg.Groups[id]
	if !ok {
		return nil, fmt.Errorf("groupe inconnu")
	}
	ids := append([]string{}, g.Servers...)
	for sid, s := range e.cfg.Servers {
		for _, x := range s.Groups {
			if x == id {
				ids = append(ids, sid)
			}
		}
	}
	return e.wakeIDs(ctx, unique(ids))
}
func (e *Engine) wakeAll(ctx context.Context) ([]WakeResult, error) {
	var ids []string
	for id, s := range e.cfg.Servers {
		if enabled(s) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return e.wakeIDs(ctx, ids)
}
func (e *Engine) wakeIDs(ctx context.Context, ids []string) ([]WakeResult, error) {
	done := map[string]bool{}
	var out []WakeResult
	for _, id := range ids {
		x, err := e.wakeTree(ctx, id, done)
		out = append(out, x...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
func unique(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func serveAPI(c *Config, j *JobStore, e *Engine) error {
	tokens, err := loadTokens(c.API.TokensFile)
	if err != nil {
		return fmt.Errorf("jetons API: %w", err)
	}
	if err := j.markInterrupted(); err != nil {
		return fmt.Errorf("revalidation des tâches interrompues: %w", err)
	}
	manager := newAutomationManager(c, j, e)
	s := &APIServer{cfg: c, jobs: j, engine: e, tokens: tokens, tokenPath: c.API.TokensFile, rates: map[string]rateEntry{}, automations: manager}
	resumeJobs(j, e, manager)
	go manager.monitor(context.Background())
	srv := http.Server{Addr: c.API.Listen, Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	printAPIBanner(os.Stdout, c)
	if isLoopbackListen(c.API.Listen) && c.API.TLSCert == "" {
		return srv.ListenAndServe()
	}
	return srv.ListenAndServeTLS(c.API.TLSCert, c.API.TLSKey)
}

func printAPIBanner(output io.Writer, c *Config) {
	scheme := "http"
	if c.API.TLSCert != "" {
		scheme = "https"
	}
	fmt.Fprintf(output, "WoL Manager API\n- écoute: %s://%s\n- base: /api/v1\n- santé: /api/v1/health\n- authentification: Authorization: Bearer <jeton>\n- spécification OpenAPI: api/openapi.yaml\n- arrêt interactif: Ctrl+C (ou sudo systemctl stop wol-manager)\n", scheme, c.API.Listen)
	if !isLoopbackListen(c.API.Listen) {
		fmt.Fprintln(output, "AVERTISSEMENT: l'écoute dépasse loopback; TLS est obligatoire et l'accès réseau doit être filtré.")
	}
}

func resumeJobs(j *JobStore, e *Engine, managers ...*AutomationManager) {
	for _, job := range j.list() {
		if job.State != "revalidation_required" {
			continue
		}
		id := job.ID
		switch job.Kind {
		case "wake":
			_ = j.resume(id, func(ctx context.Context) (any, error) { return e.wakeTree(ctx, job.Target, map[string]bool{}) })
		case "wake_group":
			_ = j.resume(id, func(ctx context.Context) (any, error) { return e.wakeGroup(ctx, job.Target) })
		case "wake_all":
			_ = j.resume(id, func(ctx context.Context) (any, error) { return e.wakeAll(ctx) })
		case "automation_restore":
			// Le coordinateur reprendra ce job seulement après une nouvelle
			// lecture NUT, un nouveau délai de stabilité et une revalidation.
			if len(managers) == 0 {
				j.update(id, "failed", "coordinateur d'automatisation indisponible", nil)
			}
		default:
			j.update(id, "failed", "reprise automatique interdite pour ce type de tâche", nil)
		}
	}
}

type relayRequest struct {
	Server    string `json:"server"`
	RequestID string `json:"request_id"`
}

func serveRelay(c *Config, e *Engine, relayID, listen, certFile, keyFile, clientCA string) error {
	if _, ok := c.Relays[relayID]; !ok {
		return fmt.Errorf("relais %q absent de la configuration", relayID)
	}
	ca, err := os.ReadFile(clientCA)
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return errors.New("CA client invalide")
	}
	mux := http.NewServeMux()
	var mu sync.Mutex
	seen := map[string]time.Time{}
	mux.HandleFunc("/relay/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			method(w)
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/relay/v1/wake", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			method(w)
			return
		}
		var b relayRequest
		if !decodeJSON(w, r, &b) {
			return
		}
		if b.RequestID == "" {
			writeError(w, 400, "invalid_request", "request_id requis")
			return
		}
		s, ok := c.Servers[b.Server]
		if !ok || !enabled(s) {
			writeError(w, 403, "forbidden", "serveur non autorisé")
			return
		}
		n := c.Networks[s.Network]
		if n.Relay != relayID {
			writeError(w, 403, "forbidden", "réseau non affecté à un relais")
			return
		}
		mu.Lock()
		if _, ok := seen[b.RequestID]; ok {
			mu.Unlock()
			writeJSON(w, 202, map[string]string{"status": "duplicate_ignored"})
			return
		}
		seen[b.RequestID] = time.Now()
		for id, t := range seen {
			if time.Since(t) > 24*time.Hour {
				delete(seen, id)
			}
		}
		mu.Unlock()
		copy := *e
		copy.noWait = true
		copy.cfg = &*c
		copy.cfg.Networks = map[string]Network{}
		for k, v := range c.Networks {
			v.Relay = ""
			v.AllowDirectedBroadcast = false
			copy.cfg.Networks[k] = v
		}
		res, er := copy.wakeOne(r.Context(), b.Server)
		if er != nil {
			writeError(w, 502, "send_failed", er.Error())
			return
		}
		writeJSON(w, 202, res)
	})
	srv := http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}}
	return srv.ListenAndServeTLS(certFile, keyFile)
}
