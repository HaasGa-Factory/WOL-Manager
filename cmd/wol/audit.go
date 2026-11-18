package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Auditor struct {
	mu        sync.Mutex
	path      string
	mirror    bool
	lastError string
	eventLog  serviceLogFunc
}

func (a *Auditor) write(event, actor, target, outcome string, details any) error {
	x := map[string]any{"time": time.Now().UTC(), "event": event, "actor": actor, "target": target, "outcome": outcome, "details": details}
	b, err := json.Marshal(x)
	if err != nil {
		return fmt.Errorf("sérialisation audit: %w", err)
	}
	if a != nil && a.mirror {
		log.Print(string(b))
	}
	if a == nil || a.path == "" {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.path), 0750); err != nil {
		a.reportError(err)
		return err
	}
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		a.reportError(err)
		return err
	}
	line := append(b, '\n')
	n, writeErr := f.Write(line)
	closeErr := f.Close()
	if writeErr == nil && n != len(line) {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		a.reportError(writeErr)
		return writeErr
	}
	if closeErr != nil {
		a.reportError(closeErr)
		return closeErr
	}
	if a.lastError != "" {
		a.logEvent("INFO", "audit: journal de nouveau accessible")
		a.lastError = ""
	}
	return nil
}

func (a *Auditor) reportError(err error) {
	message := err.Error()
	if message == a.lastError {
		return
	}
	a.lastError = message
	a.logEvent("ERROR", "audit: %v", err)
}

func (a *Auditor) logEvent(level, format string, args ...any) {
	if a.eventLog != nil {
		a.eventLog(level, format, args...)
		return
	}
	processServiceLog(level, format, args...)
}
