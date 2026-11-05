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
	mu     sync.Mutex
	path   string
	mirror bool
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
		log.Printf("ERREUR audit: %v", err)
		return err
	}
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		log.Printf("ERREUR audit: %v", err)
		return err
	}
	line := append(b, '\n')
	n, writeErr := f.Write(line)
	closeErr := f.Close()
	if writeErr == nil && n != len(line) {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		log.Printf("ERREUR audit: %v", writeErr)
		return writeErr
	}
	if closeErr != nil {
		log.Printf("ERREUR audit: %v", closeErr)
		return closeErr
	}
	return nil
}
