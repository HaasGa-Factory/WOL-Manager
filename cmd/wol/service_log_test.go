package main

import (
	"bytes"
	"testing"
)

func TestServiceLogUsesJournaldPrioritiesOnlyForJournalStream(t *testing.T) {
	var journal bytes.Buffer
	writeServiceLog(&journal, true, "WARNING", "coupure détectée; ups=%s", "ups-main")
	if got, want := journal.String(), "<4>WARNING coupure détectée; ups=ups-main\n"; got != want {
		t.Fatalf("priorité journald incorrecte: got=%q want=%q", got, want)
	}

	var terminal bytes.Buffer
	writeServiceLog(&terminal, false, "INFO", "daemon démarré")
	if got, want := terminal.String(), "INFO daemon démarré\n"; got != want {
		t.Fatalf("préfixe journald visible hors systemd: got=%q want=%q", got, want)
	}
}

func TestServiceLogMapsKnownLevels(t *testing.T) {
	for level, priority := range map[string]string{"INFO": "<6>", "WARNING": "<4>", "ERROR": "<3>"} {
		var output bytes.Buffer
		writeServiceLog(&output, true, level, "message")
		if !bytes.HasPrefix(output.Bytes(), []byte(priority+level+" ")) {
			t.Fatalf("niveau %s sans priorité %s: %q", level, priority, output.String())
		}
	}
}
