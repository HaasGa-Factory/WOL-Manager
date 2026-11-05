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
		{name: "terminal inconnu", tty: true, expected: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := allowColor(test.tty, test.noColor, test.term); got != test.expected {
				t.Fatalf("allowColor() = %v, attendu %v", got, test.expected)
			}
		})
	}
}

func TestColorModeFallbacks(t *testing.T) {
	tests := []struct {
		name, term, colorTerm string
		want                  colorMode
	}{
		{name: "truecolor", term: "xterm-256color", colorTerm: "truecolor", want: colorTrueColor},
		{name: "256 couleurs", term: "xterm-256color", want: colorANSI256},
		{name: "16 couleurs", term: "xterm", want: colorANSI16},
		{name: "Windows SSH sans TERM", want: colorANSI16},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := detectColorMode(true, false, test.term, test.colorTerm); got != test.want {
				t.Fatalf("mode = %v, attendu %v", got, test.want)
			}
		})
	}
	if got := detectColorMode(true, true, "xterm-256color", "truecolor"); got != colorNone {
		t.Fatalf("NO_COLOR ignoré: %v", got)
	}
}

func TestPaletteUsesRequestedTrueColorValues(t *testing.T) {
	tests := []struct {
		style messageStyle
		code  string
	}{
		{styleTitle, "38;2;34;211;238"},
		{styleSection, "38;2;251;191;36"},
		{styleCommand, "38;2;74;222;128"},
		{styleDescription, "38;2;209;213;219"},
		{styleMuted, "38;2;156;163;175"},
		{styleError, "38;2;248;113;113"},
		{styleInfo, "38;2;96;165;250"},
	}
	for _, test := range tests {
		if got := styledTextMode("texte", test.style, colorTrueColor); !strings.Contains(got, test.code) {
			t.Errorf("style %v sans couleur %s: %q", test.style, test.code, got)
		}
	}
}

func TestVisibleWidthIgnoresANSIAndHandlesUnicode(t *testing.T) {
	if got := visibleWidth("\x1b[1;36mWoL\x1b[0m"); got != 3 {
		t.Fatalf("largeur ANSI = %d", got)
	}
	if got := visibleWidth("é⚡"); got != 3 {
		t.Fatalf("largeur Unicode = %d", got)
	}
	if got := visibleWidth("e\u0301"); got != 1 {
		t.Fatalf("largeur combinée = %d", got)
	}
}

func TestHelpRenderingAtSupportedWidths(t *testing.T) {
	rows := []helpRow{{command: "wol config network|machine|group|nut|discovery", description: "Gestion guidée sans édition YAML et sans troncature silencieuse"}}
	for _, width := range []int{60, 80, 120, 160} {
		var output bytes.Buffer
		renderHelpRows(&output, rows, helpRenderOptions{width: width})
		for _, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
			if got := visibleWidth(line); got > width {
				t.Errorf("largeur %d: ligne de %d colonnes: %q", width, got, line)
			}
		}
		if !strings.Contains(output.String(), "wol config network|machine|group|nut|discovery") || !strings.Contains(output.String(), "troncature") {
			t.Fatalf("contenu perdu à %d colonnes: %q", width, output.String())
		}
	}
}

func TestRootHelpMatchesVisualIdentityAndCanStayPlain(t *testing.T) {
	var plain bytes.Buffer
	renderRootHelp(&plain, helpRenderOptions{mode: colorNone, width: 80})
	for _, expected := range []string{"⚡ WoL Manager v" + version, "Gestion centralisée du Wake-on-LAN", "COMMANDES PRINCIPALES", "wol wake --group GROUPE", "OPTIONS GLOBALES", "--no-color"} {
		if !strings.Contains(plain.String(), expected) {
			t.Errorf("aide sans %q:\n%s", expected, plain.String())
		}
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatal("séquence ANSI dans l'aide sans couleur")
	}

	var colored bytes.Buffer
	renderRootHelp(&colored, helpRenderOptions{mode: colorTrueColor, width: 120})
	if !strings.Contains(colored.String(), "\x1b[") || !strings.Contains(colored.String(), "38;2;34;211;238") {
		t.Fatalf("identité colorée absente: %q", colored.String())
	}
}

func TestCompleteRootHelpFitsSupportedWidths(t *testing.T) {
	for _, width := range []int{60, 80, 120, 160} {
		var output bytes.Buffer
		renderRootHelp(&output, helpRenderOptions{mode: colorNone, width: width})
		for _, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
			if got := visibleWidth(line); got > width {
				t.Errorf("aide générale à %d colonnes: ligne de %d colonnes: %q", width, got, line)
			}
		}
	}
}

func TestDetailedAndInteractiveHelpShareVisualIdentity(t *testing.T) {
	var command bytes.Buffer
	renderCommandHelpTopic(&command, "wake", commandHelp["wake"], helpRenderOptions{mode: colorTrueColor, width: 80})
	for _, expected := range []string{"AIDE WAKE", "UTILISATION", "OPTIONS", "38;2;34;211;238"} {
		if !strings.Contains(command.String(), expected) {
			t.Errorf("aide de commande sans %q:\n%s", expected, command.String())
		}
	}

	for topic := range contextHelpPages {
		for _, width := range []int{60, 80, 120, 160} {
			var output bytes.Buffer
			renderContextHelp(&output, topic, helpRenderOptions{mode: colorNone, width: width})
			text := output.String()
			if !strings.Contains(text, "WoL Manager — AIDE") || !strings.Contains(text, "Astuce") {
				t.Errorf("aide contextuelle %q incomplète à %d colonnes:\n%s", topic, width, text)
			}
			if strings.Contains(text, "\x1b[") {
				t.Errorf("ANSI dans l'aide contextuelle texte %q", topic)
			}
			for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
				if got := visibleWidth(line); got > width {
					t.Errorf("aide %q à %d colonnes: ligne de %d colonnes: %q", topic, width, got, line)
				}
			}
		}
	}
}

func TestInteractiveMenuUsesSharedVisualIdentity(t *testing.T) {
	rows := []helpRow{
		{command: "[1] Machines", description: "Consulter les machines configurées"},
		{command: "[0] Quitter", description: "Fermer le menu"},
		{command: "[?] Aide", description: "Afficher l’aide détaillée"},
	}
	var output bytes.Buffer
	printInteractiveMenu(&output, "MENU PRINCIPAL", rows)
	text := output.String()
	normalized := strings.Join(strings.Fields(text), " ")
	for _, expected := range []string{"⚡ WoL Manager — MENU PRINCIPAL", "ACTIONS", "[1] Machines", "Consulter les machines configurées", "Les flèches ne sont pas utilisées"} {
		if !strings.Contains(normalized, expected) {
			t.Fatalf("menu interactif incomplet, élément absent %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("un rendu non-TTY ne doit pas contenir d’ANSI: %q", text)
	}
	var colored bytes.Buffer
	renderInteractiveMenu(&colored, "MENU PRINCIPAL", rows, helpRenderOptions{mode: colorTrueColor, width: 80})
	for _, code := range []string{"38;2;34;211;238", "38;2;74;222;128", "38;2;209;213;219"} {
		if !strings.Contains(colored.String(), code) {
			t.Fatalf("style TrueColor %q absent du menu: %q", code, colored.String())
		}
	}
}

func TestInteractiveMenuFitsNarrowTerminal(t *testing.T) {
	rows := []helpRow{{command: "[7] Simulation WoL", description: "Simuler un réveil sans envoyer de paquet"}}
	var output bytes.Buffer
	options := helpRenderOptions{mode: colorNone, width: 40}
	renderInteractiveMenu(&output, "MENU PRINCIPAL", rows, options)
	for _, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
		if got := visibleWidth(line); got > 40 {
			t.Fatalf("ligne trop longue (%d): %q", got, line)
		}
	}
	if !strings.Contains(output.String(), "    Simuler un réveil") {
		t.Fatalf("la description doit être empilée sur terminal étroit:\n%s", output.String())
	}
}

func TestFieldHelpUsesModernRendererWithoutChangingGuidance(t *testing.T) {
	var output bytes.Buffer
	renderFieldHelp(&output, "Adresse MAC", helpRenderOptions{mode: colorNone, width: 60})
	text := output.String()
	normalized := strings.Join(strings.Fields(text), " ")
	for _, expected := range []string{"WoL Manager — AIDE CONTEXTUELLE", "DESCRIPTION", "02:00:00:00:00:01", "Astuce", "ne modifie jamais la valeur"} {
		if !strings.Contains(normalized, expected) {
			t.Errorf("aide de champ sans %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatal("ANSI dans l'aide de champ sans couleur")
	}
}

func TestNoColorArgumentIsGlobalAndRemovedBeforeParsing(t *testing.T) {
	for _, args := range [][]string{{"--no-color", "--help"}, {"wake", "--no-color", "--help"}} {
		filtered, disabled := presentationArgs(args)
		if !disabled {
			t.Fatalf("--no-color non détecté dans %v", args)
		}
		for _, arg := range filtered {
			if arg == "--no-color" {
				t.Fatalf("--no-color transmis au parseur: %v", filtered)
			}
		}
	}
	filtered, disabled := presentationArgs([]string{"--json", "list"})
	if !disabled || len(filtered) != 2 || filtered[0] != "--json" {
		t.Fatalf("le mode JSON doit rester transmis au parseur et désactiver les couleurs: %v %t", filtered, disabled)
	}
}
