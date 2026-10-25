package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type PowerCycle struct {
	UPS             string          `json:"ups"`
	State           string          `json:"state"`
	Reason          string          `json:"reason,omitempty"`
	ObservedAt      time.Time       `json:"observed_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	StableSince     *time.Time      `json:"stable_since,omitempty"`
	Battery         float64         `json:"battery_percent,omitempty"`
	BatteryKnown    bool            `json:"battery_known"`
	ArmedGroups     map[string]bool `json:"armed_groups,omitempty"`
	CompletedGroups map[string]bool `json:"completed_groups,omitempty"`
}

type restorePlan struct {
	ID            string        `json:"id"`
	Group         string        `json:"group"`
	UPS           string        `json:"ups"`
	LowBattery    int           `json:"low_battery,omitempty"`
	HighBattery   int           `json:"high_battery,omitempty"`
	NeedsBattery  bool          `json:"needs_battery"`
	StableFor     time.Duration `json:"stable_for_ns"`
	PollInterval  time.Duration `json:"poll_interval_ns"`
	Mode          string        `json:"mode"`
	Order         []string      `json:"order,omitempty"`
	FailurePolicy string        `json:"failure_policy"`
	RetryLimit    int           `json:"retry_limit"`
	RetryInterval time.Duration `json:"retry_interval_ns"`
	Priority      int           `json:"priority"`
}

type PowerGate struct {
	mu      sync.RWMutex
	blocked map[string]string
}

func newPowerGate() *PowerGate { return &PowerGate{blocked: map[string]string{}} }

func (g *PowerGate) Block(ups, reason string) {
	if ups == "" {
		return
	}
	g.mu.Lock()
	g.blocked[ups] = reason
	g.mu.Unlock()
}

func (g *PowerGate) Allow(ups string) {
	g.mu.Lock()
	delete(g.blocked, ups)
	g.mu.Unlock()
}

func (g *PowerGate) AllowServer(c *Config, id string) error {
	p := c.effectiveUPS(id)
	if p == nil {
		return nil
	}
	return g.AllowUPS(p.UPS)
}

func (g *PowerGate) AllowUPS(ups string) error {
	if ups == "" {
		return nil
	}
	g.mu.RLock()
	reason := g.blocked[ups]
	g.mu.RUnlock()
	if reason != "" {
		return upsRevalidationErrorf("réveil suspendu pour UPS %s: %s", ups, reason)
	}
	return nil
}

type AutomationManager struct {
	cfg      *Config
	jobs     *JobStore
	engine   *Engine
	gate     *PowerGate
	status   func(context.Context, string, UPSConfig) UPSStatus
	now      func() time.Time
	mu       sync.Mutex
	nextPoll map[string]time.Time
}

func newAutomationManager(c *Config, jobs *JobStore, engine *Engine) *AutomationManager {
	gate := newPowerGate()
	engine.gate = gate
	manager := &AutomationManager{cfg: c, jobs: jobs, engine: engine, gate: gate, status: nutStatus, now: time.Now, nextPoll: map[string]time.Time{}}
	for _, cycle := range jobs.listCycles() {
		if cycle.State != "completed" && cycle.State != "failed" {
			cycle.State = "waiting"
			cycle.StableSince = nil
			cycle.Reason = "service redémarré; conditions à revalider"
			cycle.UpdatedAt = manager.now()
			gate.Block(cycle.UPS, cycle.Reason)
			_ = manager.saveCycle(cycle)
		}
	}
	return manager
}

func (c *Config) restorePlans() []restorePlan {
	var plans []restorePlan
	direct := map[string]bool{}
	for groupID, group := range c.Groups {
		if group.PowerRestore == nil || !group.PowerRestore.Enabled {
			continue
		}
		p := group.PowerRestore
		low, high := defaultPowerRestoreLowBattery, defaultPowerRestoreHighBattery
		if p.LowBattery != nil {
			low = *p.LowBattery
		}
		if p.HighBattery != nil {
			high = *p.HighBattery
		}
		plans = append(plans, restorePlan{ID: "group." + groupID, Group: groupID, UPS: p.UPS, LowBattery: low, HighBattery: high, NeedsBattery: true, StableFor: p.StableFor.Value(30 * time.Second), PollInterval: p.PollInterval.Value(5 * time.Second), Mode: defaultString(p.Mode, "sequential"), Order: append([]string{}, p.Order...), FailurePolicy: defaultString(p.FailurePolicy, "stop"), RetryLimit: p.RetryLimit, RetryInterval: p.RetryInterval.Value(10 * time.Second), Priority: group.Priority})
		direct[groupID] = true
	}
	for _, legacy := range c.Automations {
		if !legacy.Enabled || legacy.Event != "nut.power_restored" || direct[legacy.Group] {
			continue
		}
		plans = append(plans, restorePlan{ID: legacy.ID, Group: legacy.Group, UPS: legacy.UPS, StableFor: legacy.StableFor.Value(30 * time.Second), PollInterval: 5 * time.Second, Mode: "sequential", FailurePolicy: "stop", Priority: c.Groups[legacy.Group].Priority})
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].Priority != plans[j].Priority {
			return plans[i].Priority < plans[j].Priority
		}
		return plans[i].ID < plans[j].ID
	})
	return plans
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (m *AutomationManager) monitor(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.poll(ctx)
		}
	}
}

func (m *AutomationManager) poll(ctx context.Context) {
	now := m.now()
	intervals := map[string]time.Duration{}
	for _, plan := range m.cfg.restorePlans() {
		if current := intervals[plan.UPS]; current == 0 || plan.PollInterval < current {
			intervals[plan.UPS] = plan.PollInterval
		}
	}
	for ups, interval := range intervals {
		if now.Before(m.nextPoll[ups]) {
			continue
		}
		m.nextPoll[ups] = now.Add(interval)
		m.handleStatus(ups, m.status(ctx, ups, m.cfg.UPS[ups]), now)
	}
}

func (m *AutomationManager) handleStatus(ups string, status UPSStatus, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	plans := m.plansForUPS(ups)
	if len(plans) == 0 {
		return
	}
	cycle, exists := m.jobs.cycle(ups)
	if cycle.ArmedGroups == nil {
		cycle.ArmedGroups = map[string]bool{}
	}
	if cycle.CompletedGroups == nil {
		cycle.CompletedGroups = map[string]bool{}
	}
	unsafe := status.Error != "" || status.State == "unknown" || status.State == "OB" || status.State == "LB" || status.State == "FSD"
	if unsafe {
		reason := status.Error
		if reason == "" {
			reason = "état NUT " + status.State
		}
		m.gate.Block(ups, reason)
		m.jobs.cancelMatching("automation_restore", ups)
		if status.State == "OB" || status.State == "LB" || status.State == "FSD" {
			if !exists || cycle.State == "completed" {
				cycle = PowerCycle{UPS: ups, State: "outage", ObservedAt: now, ArmedGroups: map[string]bool{}, CompletedGroups: map[string]bool{}}
			} else if cycle.State != "outage" {
				cycle.ObservedAt = now
				cycle.ArmedGroups = map[string]bool{}
				cycle.CompletedGroups = map[string]bool{}
			}
			cycle.State = "outage"
			cycle.StableSince = nil
			cycle.Reason = reason
			cycle.Battery, cycle.BatteryKnown = status.Battery, status.BatteryKnown
			for _, plan := range plans {
				if !plan.NeedsBattery || plan.LowBattery == 0 || status.State == "LB" || status.State == "FSD" || status.BatteryKnown && status.Battery <= float64(plan.LowBattery) {
					cycle.ArmedGroups[plan.ID] = true
				}
			}
			cycle.UpdatedAt = now
			if m.saveCycle(cycle) != nil {
				return
			}
		} else if exists && cycle.State != "completed" && cycle.State != "failed" {
			cycle.State = "waiting"
			cycle.StableSince = nil
			cycle.Reason = reason
			cycle.UpdatedAt = now
			if m.saveCycle(cycle) != nil {
				return
			}
		}
		return
	}
	if status.State != "OL" {
		m.gate.Block(ups, "état NUT non sûr "+status.State)
		return
	}
	if !exists {
		m.gate.Allow(ups)
		return
	}
	if cycle.State == "completed" {
		m.gate.Allow(ups)
		return
	}
	if cycle.State == "failed" {
		m.gate.Allow(ups)
		return
	}
	for _, plan := range plans {
		if plan.NeedsBattery && status.BatteryKnown && status.Battery <= float64(plan.LowBattery) {
			cycle.ArmedGroups[plan.ID] = true
		}
	}
	if cycle.StableSince == nil {
		stable := now
		cycle.StableSince = &stable
	}
	cycle.Battery, cycle.BatteryKnown = status.Battery, status.BatteryKnown
	cycle.State = "waiting"
	cycle.Reason = m.waitReason(cycle, plans, status, now)
	cycle.UpdatedAt = now
	if m.saveCycle(cycle) != nil {
		return
	}
	ready := m.readyPlans(cycle, plans, status, now)
	if len(ready) == 0 {
		m.gate.Block(ups, cycle.Reason)
		return
	}
	if m.jobs.activeJob("automation_restore", ups) {
		m.gate.Allow(ups)
		return
	}
	m.gate.Allow(ups)
	run := func(ctx context.Context) (any, error) {
		result, runErr := m.runPlans(ctx, ups, ready)
		if persistErr := m.finishCycle(ups, ready, runErr); runErr == nil {
			runErr = persistErr
		}
		return result, runErr
	}
	var job *Job
	var err error
	if id, resumable := m.jobs.revalidationJob("automation_restore", ups); resumable {
		err = m.jobs.resume(id, run)
		if err == nil {
			value, _ := m.jobs.get(id)
			job = &value
		}
	} else {
		job, err = m.jobs.create("automation_restore", ups, run)
	}
	if err != nil || job == nil {
		reason := "création de la tâche automatique impossible"
		if err != nil {
			reason += ": " + err.Error()
		}
		m.gate.Block(ups, reason)
		m.engine.audit.write("automation", "nut", ups, "blocked", reason)
		return
	}
	cycle.State = "running"
	cycle.Reason = "séquence automatique " + job.ID
	cycle.UpdatedAt = now
	if err := m.saveCycle(cycle); err != nil {
		_ = m.jobs.cancelJob(job.ID)
		return
	}
	m.engine.audit.write("automation", "nut", ups, "accepted", map[string]any{"job": job.ID})
}

func (m *AutomationManager) waitReason(cycle PowerCycle, plans []restorePlan, status UPSStatus, now time.Time) string {
	armed := false
	for _, plan := range plans {
		if cycle.CompletedGroups[plan.ID] || !cycle.ArmedGroups[plan.ID] {
			continue
		}
		armed = true
		if plan.NeedsBattery && !status.BatteryKnown {
			return "attente: battery.charge absente ou invalide"
		}
		if plan.NeedsBattery && status.Battery < float64(plan.HighBattery) {
			return fmt.Sprintf("attente: batterie %.0f%% < seuil haut %d%%", status.Battery, plan.HighBattery)
		}
		if cycle.StableSince == nil || now.Sub(*cycle.StableSince) < plan.StableFor {
			return "attente: stabilité secteur " + plan.StableFor.String()
		}
	}
	if !armed {
		return "attente: seuil bas de réarmement non atteint pendant le cycle"
	}
	return "conditions de reprise satisfaites"
}

func (m *AutomationManager) saveCycle(cycle PowerCycle) error {
	if err := m.jobs.setCycle(cycle); err != nil {
		reason := "persistance du cycle impossible: " + err.Error()
		m.gate.Block(cycle.UPS, reason)
		m.engine.audit.write("automation", "nut", cycle.UPS, "blocked", reason)
		return errors.New(reason)
	}
	return nil
}

func (m *AutomationManager) plansForUPS(ups string) []restorePlan {
	var result []restorePlan
	for _, plan := range m.cfg.restorePlans() {
		if plan.UPS == ups {
			result = append(result, plan)
		}
	}
	return result
}

func (m *AutomationManager) readyPlans(cycle PowerCycle, plans []restorePlan, status UPSStatus, now time.Time) []restorePlan {
	var ready []restorePlan
	for _, plan := range plans {
		if cycle.CompletedGroups[plan.ID] || !cycle.ArmedGroups[plan.ID] || cycle.StableSince == nil || now.Sub(*cycle.StableSince) < plan.StableFor {
			continue
		}
		if plan.NeedsBattery && (!status.BatteryKnown || status.Battery < float64(plan.HighBattery)) {
			continue
		}
		ready = append(ready, plan)
	}
	return ready
}

func (m *AutomationManager) finishCycle(ups string, plans []restorePlan, runErr error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cycle, ok := m.jobs.cycle(ups)
	if !ok {
		return errors.New("cycle électrique persistant introuvable")
	}
	if cycle.CompletedGroups == nil {
		cycle.CompletedGroups = map[string]bool{}
	}
	if runErr == nil {
		for _, plan := range plans {
			cycle.CompletedGroups[plan.ID] = true
		}
		pending := false
		for _, plan := range m.plansForUPS(ups) {
			if cycle.ArmedGroups[plan.ID] && !cycle.CompletedGroups[plan.ID] {
				pending = true
				break
			}
		}
		if pending {
			cycle.State = "waiting"
			cycle.Reason = "autres groupes encore en attente"
		} else {
			cycle.State = "completed"
			cycle.Reason = "séquence terminée"
		}
	} else if errors.Is(runErr, context.Canceled) {
		cycle.State = "outage"
		cycle.Reason = "séquence suspendue par une nouvelle coupure"
	} else if needsUPSRevalidation(runErr) {
		cycle.State = "waiting"
		cycle.StableSince = nil
		cycle.Reason = "reprise suspendue; nouvelle vérification NUT requise: " + runErr.Error()
		m.gate.Block(ups, cycle.Reason)
	} else {
		cycle.State = "failed"
		cycle.Reason = runErr.Error()
	}
	cycle.UpdatedAt = m.now()
	return m.saveCycle(cycle)
}

func (m *AutomationManager) runPlans(ctx context.Context, ups string, plans []restorePlan) ([]WakeResult, error) {
	coordinator := newWakeCoordinator()
	var all []WakeResult
	for index, plan := range plans {
		if err := m.updateProgress(ups, plan, index+1, len(plans)); err != nil {
			return all, err
		}
		engine := *m.engine
		engine.requireVerified = true
		engine.automationUPS = ups
		engine.coordinator = coordinator
		result, err := engine.wakeRestoreGroup(ctx, plan)
		all = append(all, result...)
		if err != nil {
			if needsUPSRevalidation(err) || plan.FailurePolicy != "continue" && plan.FailurePolicy != "continue_retry" {
				return all, fmt.Errorf("groupe %s: %w", plan.Group, err)
			}
			continue
		}
		if err := m.completePlan(ups, plan.ID); err != nil {
			return all, err
		}
	}
	return all, nil
}

func (m *AutomationManager) completePlan(ups, planID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cycle, ok := m.jobs.cycle(ups)
	if !ok {
		return errors.New("cycle électrique persistant introuvable")
	}
	if cycle.State == "outage" {
		return context.Canceled
	}
	if cycle.CompletedGroups == nil {
		cycle.CompletedGroups = map[string]bool{}
	}
	cycle.CompletedGroups[planID] = true
	cycle.UpdatedAt = m.now()
	return m.saveCycle(cycle)
}

func (m *AutomationManager) updateProgress(ups string, plan restorePlan, current, total int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cycle, ok := m.jobs.cycle(ups)
	if !ok {
		return errors.New("cycle électrique persistant introuvable")
	}
	if cycle.State == "outage" {
		return context.Canceled
	}
	cycle.State = "running"
	cycle.Reason = fmt.Sprintf("groupe %s (%d/%d)", plan.Group, current, total)
	cycle.UpdatedAt = m.now()
	return m.saveCycle(cycle)
}

func (m *AutomationManager) automationStatus() map[string]any {
	return map[string]any{"cycles": m.jobs.listCycles(), "policies": m.cfg.restorePlans(), "jobs": m.jobs.list()}
}

type wakeClaim struct {
	done chan struct{}
	err  error
}

type wakeCoordinator struct {
	mu     sync.Mutex
	claims map[string]*wakeClaim
}

func newWakeCoordinator() *wakeCoordinator { return &wakeCoordinator{claims: map[string]*wakeClaim{}} }

func (c *wakeCoordinator) acquire(ctx context.Context, id string) (bool, error) {
	c.mu.Lock()
	claim := c.claims[id]
	if claim == nil {
		c.claims[id] = &wakeClaim{done: make(chan struct{})}
		c.mu.Unlock()
		return true, nil
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-claim.done:
		return false, claim.err
	}
}

func (c *wakeCoordinator) resetFailed(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	claim := c.claims[id]
	if claim == nil || claim.err == nil {
		return
	}
	select {
	case <-claim.done:
		delete(c.claims, id)
	default:
	}
}

func (c *wakeCoordinator) complete(id string, err error) {
	c.mu.Lock()
	claim := c.claims[id]
	claim.err = err
	close(claim.done)
	c.mu.Unlock()
}

func (e *Engine) wakeRestoreGroup(ctx context.Context, plan restorePlan) ([]WakeResult, error) {
	ids := e.cfg.orderedGroupMembers(plan.Group, plan.Order)
	if plan.Mode != "parallel" {
		var all []WakeResult
		done := map[string]bool{}
		for _, id := range ids {
			result, err := e.wakeWithRetries(ctx, id, done, plan)
			all = append(all, result...)
			if err != nil && plan.FailurePolicy != "continue" && plan.FailurePolicy != "continue_retry" {
				return all, err
			}
		}
		return all, nil
	}
	type outcome struct {
		results []WakeResult
		err     error
	}
	channel := make(chan outcome, len(ids))
	for _, id := range ids {
		id := id
		go func() {
			result, err := e.wakeWithRetries(ctx, id, map[string]bool{}, plan)
			channel <- outcome{result, err}
		}()
	}
	var all []WakeResult
	var firstErr error
	for range ids {
		outcome := <-channel
		all = append(all, outcome.results...)
		if outcome.err != nil && firstErr == nil {
			firstErr = outcome.err
		}
	}
	if firstErr != nil && plan.FailurePolicy != "continue" && plan.FailurePolicy != "continue_retry" {
		return all, firstErr
	}
	return all, nil
}

func (e *Engine) wakeWithRetries(ctx context.Context, id string, done map[string]bool, plan restorePlan) ([]WakeResult, error) {
	attempts := 1
	if plan.FailurePolicy == "retry" || plan.FailurePolicy == "continue_retry" {
		attempts += plan.RetryLimit
	}
	var all []WakeResult
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			e.resetCoordinatorFailures(id, map[string]bool{})
		}
		result, err := e.wakeTree(ctx, id, done)
		all = append(all, result...)
		if err == nil {
			return all, nil
		}
		lastErr = err
		if attempt+1 < attempts {
			if err := waitContext(ctx, plan.RetryInterval); err != nil {
				return all, err
			}
		}
	}
	return all, lastErr
}

func (e *Engine) resetCoordinatorFailures(id string, seen map[string]bool) {
	if e.coordinator == nil || seen[id] {
		return
	}
	seen[id] = true
	e.coordinator.resetFailed(id)
	for _, dependency := range e.cfg.dependencies(id) {
		e.resetCoordinatorFailures(dependency, seen)
	}
}

func (c *Config) orderedGroupMembers(groupID string, preferred []string) []string {
	seen := map[string]bool{}
	var members []string
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			members = append(members, id)
		}
	}
	for _, id := range preferred {
		add(id)
	}
	var remaining []string
	for _, id := range c.Groups[groupID].Servers {
		if !seen[id] {
			remaining = append(remaining, id)
		}
	}
	for id, server := range c.Servers {
		for _, candidate := range server.Groups {
			if candidate == groupID && !seen[id] {
				remaining = append(remaining, id)
			}
		}
	}
	sort.Strings(remaining)
	for _, id := range remaining {
		add(id)
	}
	return members
}
