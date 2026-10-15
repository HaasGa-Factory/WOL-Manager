package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Auditor struct {
	mu   sync.Mutex
	path string
}

func (a *Auditor) write(event, actor, target, outcome string, details any) {
	x := map[string]any{"time": time.Now().UTC(), "event": event, "actor": actor, "target": target, "outcome": outcome, "details": details}
	b, _ := json.Marshal(x)
	log.Print(string(b))
	if a == nil || a.path == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if os.MkdirAll(filepath.Dir(a.path), 0750) != nil {
		return
	}
	f, e := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if e == nil {
		defer f.Close()
		_, _ = f.Write(append(b, '\n'))
	}
}
