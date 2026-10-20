package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditorWritesFileWithoutMirroringJSONToProcessLog(t *testing.T) {
	var processLog bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&processLog)
	t.Cleanup(func() { log.SetOutput(previous) })

	path := filepath.Join(t.TempDir(), "audit.jsonl")
	(&Auditor{path: path}).write("wake", "cli", "two", "dry_run", map[string]bool{"dry_run": true})

	if processLog.Len() != 0 {
		t.Fatalf("l'audit a ete duplique dans la sortie du processus: %s", processLog.String())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(b), &event); err != nil {
		t.Fatalf("audit persistant invalide: %v", err)
	}
	if event["event"] != "wake" || event["outcome"] != "dry_run" {
		t.Fatalf("audit persistant incomplet: %#v", event)
	}
}

func TestAuditorMirrorsServiceEventsWhenRequested(t *testing.T) {
	var processLog bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&processLog)
	t.Cleanup(func() { log.SetOutput(previous) })

	(&Auditor{mirror: true}).write("api.wake", "client", "two", "accepted", nil)
	if !bytes.Contains(processLog.Bytes(), []byte(`"event":"api.wake"`)) {
		t.Fatalf("audit du service absent du journal du processus: %s", processLog.String())
	}
}
