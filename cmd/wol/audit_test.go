package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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

func TestAuditorReturnsWriteErrors(t *testing.T) {
	var processLog bytes.Buffer

	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	auditor := &Auditor{path: filepath.Join(parent, "audit.jsonl"), eventLog: func(level, format string, args ...any) {
		processLog.WriteString(level + " " + fmt.Sprintf(format, args...) + "\n")
	}}
	if err := auditor.write("wake", "tester", "one", "requested", nil); err == nil {
		t.Fatal("erreur d'écriture audit silencieusement ignorée")
	}
	if err := auditor.write("wake", "tester", "one", "requested", nil); err == nil {
		t.Fatal("seconde erreur d'écriture audit silencieusement ignorée")
	}
	if count := strings.Count(processLog.String(), "ERROR audit:"); count != 1 {
		t.Fatalf("erreur d'audit répétée pendant le polling: count=%d log=%q", count, processLog.String())
	}
}
