package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestStyledText(t *testing.T) {
	plain := "AVERTISSEMENT: le canal par défaut reste main"
	if got := styledText(plain, styleWarning, false); got != plain {
		t.Fatalf("sortie sans couleur modifiée: %q", got)
	}
	got := styledText(plain, styleWarning, true)
	if !strings.HasPrefix(got, "\x1b[1;33m") || !strings.HasSuffix(got, "\x1b[0m") || !strings.Contains(got, plain) {
		t.Fatalf("avertissement coloré incorrect: %q", got)
	}
}

func TestMessageToNonTerminalStaysPlain(t *testing.T) {
	var output bytes.Buffer
	errorf(&output, "ERREUR: %s", "échec")
	if got := output.String(); got != "ERREUR: échec\n" {
		t.Fatalf("sortie redirigée incompatible: %q", got)
	}
}

func TestNoColorDisablesTerminalColors(t *testing.T) {
	old, existed := os.LookupEnv("NO_COLOR")
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv("NO_COLOR", old)
		} else {
			_ = os.Unsetenv("NO_COLOR")
		}
	})
	if err := os.Setenv("NO_COLOR", "1"); err != nil {
		t.Fatal(err)
	}
	if colorEnabled(os.Stdout) {
		t.Fatal("NO_COLOR n'a pas désactivé les couleurs")
	}
}

func TestColorCapability(t *testing.T) {
	for _, test := range []struct {
		name         string
		tty, noColor bool
		term         string
		expected     bool
	}{
		{name: "terminal", tty: true, term: "xterm-256color", expected: true},
		{name: "redirection", tty: false, term: "xterm-256color"},
		{name: "NO_COLOR", tty: true, noColor: true, term: "xterm-256color"},
		{name: "terminal dumb", tty: true, term: "dumb"},
		{name: "terminal inconnu", tty: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := allowColor(test.tty, test.noColor, test.term); got != test.expected {
				t.Fatalf("allowColor() = %v, attendu %v", got, test.expected)
			}
		})
	}
}
