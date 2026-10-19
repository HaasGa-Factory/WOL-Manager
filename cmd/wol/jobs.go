package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type Job struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Target  string    `json:"target"`
	State   string    `json:"state"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	Message string    `json:"message,omitempty"`
	Result  any       `json:"result,omitempty"`
}
type jobFile struct {
	Jobs   map[string]*Job        `json:"jobs"`
	Cycles map[string]*PowerCycle `json:"power_cycles,omitempty"`
}
type JobStore struct {
	mu     sync.Mutex
	path   string
	jobs   map[string]*Job
	cancel map[string]context.CancelFunc
	cycles map[string]*PowerCycle
}

func openJobs(path string) (*JobStore, error) {
	s := &JobStore{path: path, jobs: map[string]*Job{}, cancel: map[string]context.CancelFunc{}, cycles: map[string]*PowerCycle{}}
	b, e := os.ReadFile(path)
	if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	if len(b) > 0 {
		var f jobFile
		if e = json.Unmarshal(b, &f); e != nil {
			return nil, fmt.Errorf("état des tâches: %w", e)
		}
		s.jobs = f.Jobs
		if s.jobs == nil {
			s.jobs = map[string]*Job{}
		}
		if f.Cycles != nil {
			s.cycles = f.Cycles
		}
	}
	return s, nil
}

func (s *JobStore) markInterrupted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, job := range s.jobs {
		if job.State == "running" || job.State == "queued" {
			job.State = "revalidation_required"
			job.Message = "service redémarré; reprise interdite avant revalidation"
			job.Updated = time.Now().UTC()
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveLocked()
}
func newID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:]), nil
}
func (s *JobStore) saveLocked() error {
	dir := filepath.Dir(s.path)
	if e := os.MkdirAll(dir, 0750); e != nil {
		return e
	}
	if e := secureStateDir(dir); e != nil {
		return e
	}
	b, e := json.MarshalIndent(jobFile{Jobs: s.jobs, Cycles: s.cycles}, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".state-*.tmp")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(0640); e == nil {
		_, e = f.Write(b)
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = secureStateFile(tmp); e != nil {
		return e
	}
	if e = os.Rename(tmp, s.path); e == nil {
		return nil
	}
	// Windows n'autorise pas toujours le remplacement par renommage. La cible
	// de production est Linux, où le chemin ci-dessus reste atomique.
	if runtime.GOOS == "windows" {
		if removeErr := os.Remove(s.path); removeErr != nil && !os.IsNotExist(removeErr) {
			return e
		}
		return os.Rename(tmp, s.path)
	}
	return e
}
func (s *JobStore) create(kind, target string, fn func(context.Context) (any, error)) (*Job, error) {
	s.mu.Lock()
	for _, j := range s.jobs {
		if j.Kind == kind && j.Target == target && (j.State == "queued" || j.State == "running" || j.State == "revalidation_required") {
			s.mu.Unlock()
			return nil, fmt.Errorf("une tâche %s pour %s est déjà active: %s", kind, target, j.ID)
		}
	}
	now := time.Now().UTC()
	id, idErr := newID()
	if idErr != nil {
		s.mu.Unlock()
		return nil, idErr
	}
	j := &Job{ID: id, Kind: kind, Target: target, State: "queued", Created: now, Updated: now}
	s.jobs[j.ID] = j
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel[j.ID] = cancel
	if e := s.saveLocked(); e != nil {
		delete(s.jobs, j.ID)
		delete(s.cancel, j.ID)
		s.mu.Unlock()
		return nil, e
	}
	copy := *j
	s.mu.Unlock()
	go func() {
		s.update(j.ID, "running", "", nil)
		v, e := fn(ctx)
		if e != nil {
			state := "failed"
			if ctx.Err() != nil {
				state = "cancelled"
			}
			s.update(j.ID, state, e.Error(), v)
		} else {
			s.update(j.ID, "completed", "", v)
		}
	}()
	return &copy, nil
}

func (s *JobStore) resume(id string, fn func(context.Context) (any, error)) error {
	s.mu.Lock()
	j := s.jobs[id]
	if j == nil || j.State != "revalidation_required" {
		s.mu.Unlock()
		return fmt.Errorf("tâche non reprenable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel[id] = cancel
	j.State = "running"
	j.Message = "conditions en cours de revalidation"
	j.Updated = time.Now().UTC()
	_ = s.saveLocked()
	s.mu.Unlock()
	go func() {
		v, e := fn(ctx)
		if e != nil {
			state := "failed"
			if ctx.Err() != nil {
				state = "cancelled"
			}
			s.update(id, state, e.Error(), v)
		} else {
			s.update(id, "completed", "", v)
		}
	}()
	return nil
}
func (s *JobStore) update(id, state, msg string, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j := s.jobs[id]; j != nil {
		if terminalJobState(j.State) {
			return
		}
		j.State = state
		j.Message = msg
		j.Result = result
		j.Updated = time.Now().UTC()
		if state != "queued" && state != "running" {
			delete(s.cancel, id)
		}
		_ = s.saveLocked()
	}
}
func (s *JobStore) get(id string) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, false
	}
	b := *j
	return b, true
}
func (s *JobStore) list() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, *j)
	}
	return out
}

func (s *JobStore) revalidationJob(kind, target string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, job := range s.jobs {
		if job.Kind == kind && job.Target == target && job.State == "revalidation_required" {
			return id, true
		}
	}
	return "", false
}

func (s *JobStore) activeJob(kind, target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.jobs {
		if job.Kind == kind && job.Target == target && (job.State == "queued" || job.State == "running") {
			return true
		}
	}
	return false
}
func (s *JobStore) cancelJob(id string) error {
	s.mu.Lock()
	j := s.jobs[id]
	if j == nil {
		s.mu.Unlock()
		return fmt.Errorf("tâche inconnue")
	}
	f := s.cancel[id]
	if f == nil {
		s.mu.Unlock()
		return fmt.Errorf("tâche non annulable dans l'état %s", j.State)
	}
	previous := *j
	j.State = "cancelled"
	j.Message = "annulation demandée"
	j.Updated = time.Now().UTC()
	delete(s.cancel, id)
	if e := s.saveLocked(); e != nil {
		*j = previous
		s.cancel[id] = f
		s.mu.Unlock()
		return e
	}
	s.mu.Unlock()
	f()
	return nil
}

func (s *JobStore) cancelMatching(kind, target string) {
	var ids []string
	s.mu.Lock()
	for id, job := range s.jobs {
		if job.Kind == kind && job.Target == target && s.cancel[id] != nil {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		_ = s.cancelJob(id)
	}
}

func terminalJobState(state string) bool {
	switch state {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (s *JobStore) setCycle(cycle PowerCycle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := clonePowerCycle(cycle)
	s.cycles[cycle.UPS] = &copy
	return s.saveLocked()
}

func (s *JobStore) cycle(ups string) (PowerCycle, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cycle, ok := s.cycles[ups]
	if !ok {
		return PowerCycle{}, false
	}
	return clonePowerCycle(*cycle), true
}

func (s *JobStore) listCycles() []PowerCycle {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PowerCycle, 0, len(s.cycles))
	for _, cycle := range s.cycles {
		out = append(out, clonePowerCycle(*cycle))
	}
	return out
}

func clonePowerCycle(cycle PowerCycle) PowerCycle {
	copy := cycle
	copy.ArmedGroups = map[string]bool{}
	for id, value := range cycle.ArmedGroups {
		copy.ArmedGroups[id] = value
	}
	copy.CompletedGroups = map[string]bool{}
	for id, value := range cycle.CompletedGroups {
		copy.CompletedGroups[id] = value
	}
	return copy
}
