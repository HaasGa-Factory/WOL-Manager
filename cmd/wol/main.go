package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	version        = "1.2.1"
	sourceRevision = "unknown"
	buildChannel   = "unknown"
)

var errUsage = errors.New("usage invalide")

func main() {
	if err := run(os.Args[1:]); err != nil {
		errorf(os.Stderr, "ERREUR: %v", err)
		if errors.Is(err, errUsage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
func run(args []string) error {
	args, cliNoColor = presentationArgs(args)
	root := flag.NewFlagSet("wol", flag.ContinueOnError)
	configPath := root.String("config", envOr("WOL_CONFIG", "/etc/wol-manager/wol.yaml"), "fichier de configuration")
	jsonOut := root.Bool("json", false, "sortie JSON")
	root.SetOutput(io.Discard)
	if err := root.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(os.Stdout)
			return nil
		}
		return errUsage
	}
	args = root.Args()
	if len(args) == 0 {
		return usage()
	}
	if wantsHelp(args) {
		return printCommandHelp(os.Stdout, args)
	}
	cmd := args[0]
	args = args[1:]
	if cmd == "version" {
		if len(args) == 1 {
			switch args[0] {
			case "--source-revision", "--commit":
				fmt.Println(sourceRevision)
				return nil
			case "--channel":
				fmt.Println(buildChannel)
				return nil
			default:
				return errUsage
			}
		}
		if len(args) != 0 {
			return errUsage
		}
		fmt.Println(version)
		return nil
	}
	if cmd == "setup" {
		return setup(*configPath)
	}
	if cmd == "update" {
		return updateCLI(args)
	}
	if cmd == "completion" {
		if len(args) != 1 || args[0] != "bash" {
			return errUsage
		}
		fmt.Print(bashCompletion)
		return nil
	}
	if cmd == "config" && len(args) > 0 && args[0] == "edit" {
		return editConfig(*configPath)
	}
	if cmd == "config" && len(args) == 1 {
		switch args[0] {
		case "network", "networks", "machine", "server", "servers", "group", "groups", "nut", "ups", "discovery":
			return configSection(*configPath, args[0], os.Stdin, os.Stdout)
		}
	}
	c, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if cmd == "config" && len(args) > 0 && args[0] == "check" {
		issues := readinessIssues(c)
		if len(issues) > 0 {
			if err := output(*jsonOut, map[string]any{"valid": true, "ready": false, "version": c.Version, "issues": issues}, ""); err != nil {
				return err
			}
			return fmt.Errorf("configuration syntaxiquement valide mais non opérationnelle: %s", strings.Join(issues, "; "))
		}
		return output(*jsonOut, map[string]any{"valid": true, "ready": true, "version": c.Version}, "configuration syntaxiquement valide et opérationnelle")
	}
	if cmd == "token" {
		return tokenCLI(c, args)
	}
	jobs, err := openJobs(c.Storage.StateFile)
	if err != nil {
		return err
	}
	audit := &Auditor{path: c.Storage.AuditFile, mirror: cmd == "api"}
	engine := &Engine{cfg: c, audit: audit, sendPacket: sendMagic}
	switch cmd {
	case "list":
		ids := make([]string, 0, len(c.Servers))
		for id := range c.Servers {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if *jsonOut {
			return output(true, c.Servers, "")
		}
		for _, id := range ids {
			fmt.Printf("%-20s %-15s %s\n", id, c.Servers[id].IP, c.Servers[id].Name)
		}
		return nil
	case "status":
		if len(args) != 1 {
			return errUsage
		}
		result := checkServer(context.Background(), c, args[0])
		if err := output(*jsonOut, result, ""); err != nil {
			return err
		}
		if _, known := c.Servers[args[0]]; !known {
			return errors.New("serveur inconnu")
		}
		return nil
	case "wake":
		return wakeCLI(c, engine, args, *jsonOut)
	case "network":
		return networkCLI(c, audit, args, *jsonOut)
	case "ups":
		if len(args) != 1 || (args[0] != "status" && args[0] != "test") {
			return errUsage
		}
		out := map[string]UPSStatus{}
		failed := false
		for id, u := range c.UPS {
			out[id] = nutStatus(context.Background(), id, u)
			failed = failed || out[id].Error != ""
		}
		if err := output(*jsonOut, out, ""); err != nil {
			return err
		}
		if args[0] == "test" {
			if len(out) == 0 {
				return errors.New("aucun UPS NUT configuré")
			}
			if failed {
				return errors.New("un ou plusieurs diagnostics NUT ont échoué")
			}
		}
		return nil
	case "jobs":
		return output(true, jobs.list(), "")
	case "automation":
		if len(args) != 1 || args[0] != "status" {
			return errUsage
		}
		return output(true, map[string]any{"cycles": jobs.listCycles(), "policies": c.restorePlans(), "jobs": jobs.list()}, "")
	case "api":
		if len(args) != 1 || args[0] != "serve" {
			return errUsage
		}
		return serveAPI(c, jobs, engine)
	case "relay":
		return relayCLI(c, engine, args)
	case "menu":
		return menu(*configPath, c, engine, *jsonOut)
	default:
		return usage()
	}
}

func usage() error {
	printUsage(os.Stderr)
	return errUsage
}

func printUsage(output io.Writer) {
	renderRootHelp(output, helpRenderOptions{mode: outputColorMode(output), width: outputWidth(output)})
}

func presentationArgs(args []string) ([]string, bool) {
	filtered := make([]string, 0, len(args))
	disabled := false
	for _, arg := range args {
		if arg == "--no-color" {
			disabled = true
			continue
		}
		if arg == "--json" {
			disabled = true
		}
		filtered = append(filtered, arg)
	}
	return filtered, disabled
}

const bashCompletion = `# bash completion for WoL Manager
_wol() {
    local cur cmd i
    cur="${COMP_WORDS[COMP_CWORD]}"
    for ((i=1; i<COMP_CWORD; i++)); do
        case "${COMP_WORDS[i]}" in
            --config) ((i++)) ;;
			--json|--no-color) ;;
            *) cmd="${COMP_WORDS[i]}"; break ;;
        esac
    done
    if [[ -z "$cmd" ]]; then
		COMPREPLY=( $(compgen -W "setup update list status wake config token network ups jobs automation menu api relay version completion --config --json --no-color" -- "$cur") )
        return
    fi
    case "$cmd" in
        update)
            if [[ "${COMP_WORDS[COMP_CWORD-1]}" == "--channel" ]]; then
                COMPREPLY=( $(compgen -W "main dev" -- "$cur") )
            else
				COMPREPLY=( $(compgen -W "--check --channel --help -h" -- "$cur") )
            fi ;;
		config) COMPREPLY=( $(compgen -W "check edit network machine group nut --help -h" -- "$cur") $(compgen -W "discovery" -- "$cur") ) ;;
		wake) COMPREPLY=( $(compgen -W "--dry-run --verification-delay --group --all --help -h" -- "$cur") ) ;;
		network) COMPREPLY=( $(compgen -W "test discover --help -h" -- "$cur") ) ;;
		ups) COMPREPLY=( $(compgen -W "status test --help -h" -- "$cur") ) ;;
		automation) COMPREPLY=( $(compgen -W "status --help -h" -- "$cur") ) ;;
		token) COMPREPLY=( $(compgen -W "create --help -h" -- "$cur") $(compgen -W "list revoke" -- "$cur") ) ;;
		api) COMPREPLY=( $(compgen -W "serve --help -h" -- "$cur") ) ;;
		relay) COMPREPLY=( $(compgen -W "serve check --help -h" -- "$cur") ) ;;
		completion) COMPREPLY=( $(compgen -W "bash --help -h" -- "$cur") ) ;;
		version) COMPREPLY=( $(compgen -W "--channel --source-revision --commit --help -h" -- "$cur") ) ;;
		*) COMPREPLY=( $(compgen -W "--help -h" -- "$cur") ) ;;
    esac
}
complete -F _wol wol
`

func wakeCLI(c *Config, e *Engine, args []string, jsonOut bool) error {
	args = flagsFirst(args, map[string]bool{"--group": true, "--verification-delay": true})
	fs := flag.NewFlagSet("wake", flag.ContinueOnError)
	group := fs.String("group", "", "groupe")
	all := fs.Bool("all", false, "tous")
	dry := fs.Bool("dry-run", false, "simulation")
	verificationDelay := fs.String("verification-delay", "", "attente avant la première vérification")
	if er := fs.Parse(args); er != nil {
		return errUsage
	}
	selectors := fs.NArg()
	if *group != "" {
		selectors++
	}
	if *all {
		selectors++
	}
	if selectors != 1 || fs.NArg() > 1 {
		return fmt.Errorf("%w: MACHINE, --group et --all sont mutuellement exclusifs; consultez « wol wake --help »", errUsage)
	}
	if *verificationDelay != "" {
		delay, er := time.ParseDuration(*verificationDelay)
		if er != nil || delay < 0 || delay > maxVerificationDuration {
			return fmt.Errorf("verification-delay doit être une durée Go comprise entre 0s et 24h")
		}
		e.verificationDelay = &delay
	}
	e.dry = *dry
	if !jsonOut {
		e.progress = os.Stdout
	}
	var v any
	var er error
	switch {
	case *group != "":
		v, er = e.wakeGroup(context.Background(), *group)
	case *all:
		v, er = e.wakeAll(context.Background())
	case fs.NArg() == 1:
		v, er = e.wakeTree(context.Background(), fs.Arg(0), map[string]bool{})
	default:
		return errUsage
	}
	if er != nil {
		return er
	}
	return output(jsonOut, v, "")
}

func flagsFirst(args []string, takesValue map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if takesValue[args[i]] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	return append(flags, positional...)
}
func networkCLI(c *Config, audit *Auditor, args []string, jsonOut bool) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "test":
		out := map[string]any{}
		failed := false
		ids := args[1:]
		if len(ids) == 0 {
			for id := range c.Servers {
				ids = append(ids, id)
			}
		}
		for _, id := range ids {
			p, e := transportPlan(c, id)
			out[id] = map[string]any{"plan": p, "error": errorString(e)}
			failed = failed || e != nil || len(p) == 0
		}
		if err := output(true, out, ""); err != nil {
			return err
		}
		if failed {
			return errors.New("un ou plusieurs plans réseau n'ont pas pu être produits")
		}
		return nil
	case "discover":
		fs := flag.NewFlagSet("discover", flag.ContinueOnError)
		active := fs.Bool("active", false, "diagnostic actif")
		allow := fs.Bool("allow-active-scan", false, "autorisation explicite non interactive")
		dry := fs.Bool("dry-run", false, "simulation")
		if e := fs.Parse(args[1:]); e != nil {
			return errUsage
		}
		if !*active {
			if !c.Discovery.PassiveEnabled {
				return errors.New("découverte passive désactivée dans la configuration")
			}
			return output(jsonOut, passiveDiscovery(context.Background()), "")
		}
		if !c.Discovery.ActiveEnabled {
			return errors.New("découverte active désactivée: lancez « sudo wol config discovery » pour définir les sous-réseaux, limites, débit et timeout; --allow-active-scan autorise seulement cet appel et n'active pas la fonction globalement")
		}
		warningf(os.Stderr, "AVERTISSEMENT: diagnostic réseau actif limité aux sous-réseaux explicitement autorisés")
		if !*allow {
			info, statErr := os.Stdin.Stat()
			if statErr != nil || info.Mode()&os.ModeCharDevice == 0 {
				return errors.New("découverte active refusée en mode non interactif: utilisez --allow-active-scan")
			}
			fmt.Fprint(os.Stderr, "Tapez ACTIVE pour confirmer: ")
			answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.TrimSpace(answer) != "ACTIVE" {
				return errors.New("découverte active annulée")
			}
		}
		audit.write("network.discovery.active", "cli", "configured_networks", "started", map[string]bool{"dry_run": *dry})
		ctx, cancel := context.WithTimeout(context.Background(), c.Discovery.Safeguards.Timeout.Value(30*time.Second))
		defer cancel()
		report := activeDiscovery(ctx, c, *dry)
		audit.write("network.discovery.active", "cli", "configured_networks", "completed", report)
		return output(jsonOut, report, "")
	default:
		return errUsage
	}
}
func relayCLI(c *Config, e *Engine, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: consultez « wol relay --help »", errUsage)
	}
	switch args[0] {
	case "serve":
		fs := flag.NewFlagSet("relay serve", flag.ContinueOnError)
		id := fs.String("id", "", "identifiant du relais configuré")
		listen := fs.String("listen", "127.0.0.1:9443", "")
		cert := fs.String("cert", "", "")
		key := fs.String("key", "", "")
		ca := fs.String("client-ca", "", "")
		if fs.Parse(args[1:]) != nil || *id == "" || *cert == "" || *key == "" || *ca == "" {
			return fmt.Errorf("%w: wol relay serve --id ID --cert FILE --key FILE --client-ca FILE [--listen ADRESSE]; consultez « wol relay serve --help »", errUsage)
		}
		return serveRelay(c, e, *id, *listen, *cert, *key, *ca)
	case "check":
		if len(args) != 2 {
			return errUsage
		}
		r, ok := c.Relays[args[1]]
		if !ok {
			return fmt.Errorf("relais %q inconnu; déclarez-le dans la section relays de la configuration ou consultez « wol relay --help »", args[1])
		}
		ctx, cancel := context.WithTimeout(context.Background(), r.Timeout.Value(5*time.Second))
		defer cancel()
		return e.sendRelayHealth(ctx, args[1])
	default:
		return errUsage
	}
}
func (e *Engine) sendRelayHealth(ctx context.Context, id string) error {
	return relayHealth(ctx, e.cfg.Relays[id])
}
func output(asJSON bool, v any, text string) error {
	if asJSON || text == "" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	successf(os.Stdout, "%s", text)
	return nil
}
func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func editConfig(path string) error {
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	after, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		fmt.Println("Aucune modification enregistrée.")
	}
	return nil
}
func menu(configPath string, c *Config, e *Engine, jsonOut bool) error {
	r := bufio.NewReader(os.Stdin)
	interactive := isTerminalFile(os.Stdin) && isTerminalFile(os.Stdout)
	for {
		printInteractiveMenu(os.Stdout, "MENU PRINCIPAL", []helpRow{
			{command: "[1] Machines", description: "Consulter les machines configurées"},
			{command: "[2] Réseaux", description: "Consulter les réseaux Wake-on-LAN"},
			{command: "[3] Groupes", description: "Consulter les groupes et dépendances"},
			{command: "[4] UPS", description: "Consulter l’état des onduleurs NUT"},
			{command: "[5] Valider", description: "Valider la configuration actuelle"},
			{command: "[6] Configurer", description: "Ouvrir l’assistant de configuration"},
			{command: "[7] Simulation WoL", description: "Simuler un réveil sans envoyer de paquet"},
			{command: "[8] Réveil réel", description: "Envoyer un paquet après confirmation explicite"},
			{command: "[9] Automatismes", description: "Consulter les reprises NUT, cycles et tâches"},
			{command: "[0] Quitter", description: "Fermer le menu"},
			{command: "[?] Aide", description: "Afficher l’aide détaillée du menu"},
		})
		fmt.Print("> ")
		x, _ := r.ReadString('\n')
		switch strings.TrimSpace(x) {
		case "0":
			return nil
		case "1":
			printInteractiveViewHeader(os.Stdout, "MACHINES")
			if jsonOut {
				_ = output(true, c.Servers, "")
			} else {
				printMenuMachines(os.Stdout, c)
			}
			if !waitForMenuReturn(r, os.Stdout, interactive, "machine") {
				return nil
			}
		case "2":
			printInteractiveViewHeader(os.Stdout, "RÉSEAUX")
			if jsonOut {
				_ = output(true, c.Networks, "")
			} else {
				printNetworks(os.Stdout, c.Networks)
			}
			if !waitForMenuReturn(r, os.Stdout, interactive, "network") {
				return nil
			}
		case "3":
			printInteractiveViewHeader(os.Stdout, "GROUPES")
			if jsonOut {
				_ = output(true, c.Groups, "")
			} else {
				printGroups(os.Stdout, c.Groups)
			}
			if !waitForMenuReturn(r, os.Stdout, interactive, "group") {
				return nil
			}
		case "4":
			printInteractiveViewHeader(os.Stdout, "UPS")
			out := map[string]UPSStatus{}
			for id, u := range c.UPS {
				out[id] = nutStatus(context.Background(), id, u)
			}
			if jsonOut {
				_ = output(true, out, "")
			} else {
				printMenuUPS(os.Stdout, out)
			}
			if !waitForMenuReturn(r, os.Stdout, interactive, "nut") {
				return nil
			}
		case "5":
			printInteractiveViewHeader(os.Stdout, "VALIDATION")
			if err := c.Validate(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println("configuration valide")
			}
			if !waitForMenuReturn(r, os.Stdout, interactive, "menu") {
				return nil
			}
		case "6":
			if err := setupWithIO(configPath, r, os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			updated, err := LoadConfig(configPath)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			c = updated
			e.cfg = updated
		case "7":
			id, ok := selectMenuServer(r, os.Stdout, c)
			if !ok {
				fmt.Println("Simulation annulée: aucune machine sélectionnée.")
				continue
			}
			dryEngine := &Engine{cfg: c, audit: e.audit, dry: true, noWait: true}
			v, err := dryEngine.wakeTree(context.Background(), id, map[string]bool{})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				if jsonOut {
					_ = output(true, v, "")
				} else {
					printWakeResults(os.Stdout, v)
				}
			}
		case "8":
			id, ok := selectMenuServer(r, os.Stdout, c)
			if !ok {
				fmt.Println("Réveil annulé: aucune machine sélectionnée.")
				continue
			}
			if _, disabled := c.effectiveCheckPolicy(id); disabled {
				fmt.Printf("Aucune vérification configurée pour %s; le réveil ne pourra pas confirmer son démarrage.\n", id)
			} else {
				_, timeout, _ := c.effectiveVerification(id)
				fmt.Printf("Contrôle préalable de %s en cours, délai maximal %s…\n", id, frenchDuration(timeout))
			}
			status := checkServer(context.Background(), c, id)
			fmt.Printf("État courant de %s: %s\n", id, status.State)
			if !confirmPrompt(r, os.Stdout, "Envoyer réellement un paquet WoL à "+id+" ?") {
				fmt.Println("Réveil annulé.")
				continue
			}
			v, err := e.wakeTree(context.Background(), id, map[string]bool{})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				if jsonOut {
					_ = output(true, v, "")
				} else {
					printWakeResults(os.Stdout, v)
				}
			}
		case "9":
			printInteractiveViewHeader(os.Stdout, "AUTOMATISMES")
			jobs, err := openJobs(c.Storage.StateFile)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else if jsonOut {
				_ = output(true, map[string]any{"cycles": jobs.listCycles(), "policies": c.restorePlans(), "jobs": jobs.list()}, "")
			} else {
				printMenuAutomations(os.Stdout, c.restorePlans(), jobs.listCycles(), jobs.list())
			}
			if !waitForMenuReturn(r, os.Stdout, interactive, "menu") {
				return nil
			}
		case "?", "aide", "help":
			printContextHelp(os.Stdout, "menu")
			if !waitForMenuReturn(r, os.Stdout, interactive, "menu") {
				return nil
			}
		default:
			fmt.Println("Choix invalide.")
		}
	}
}

func waitForMenuReturn(reader *bufio.Reader, output io.Writer, interactive bool, helpTopic string) bool {
	if !interactive {
		return false
	}
	for {
		fmt.Fprint(output, "\nAppuyez sur Entrée pour revenir au menu principal: ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "0", "retour", "annuler":
			return true
		case "?", "aide", "help":
			printContextHelp(output, helpTopic)
		default:
			fmt.Fprintln(output, "Réponse invalide: appuyez sur Entrée, ou saisissez retour, annuler, 0 ou ?.")
		}
	}
}

func printWakeResults(output io.Writer, results []WakeResult) {
	for index, result := range results {
		if index > 0 {
			fmt.Fprintln(output)
		}
		packet := result.Packet
		switch packet {
		case "dry_run":
			packet = "simulation, aucun paquet émis"
		case "sent":
			packet = "paquet annoncé comme émis"
		case "not_sent":
			packet = "aucun paquet émis"
		}
		rows := []helpRow{{command: "Paquet", description: packet}}
		if result.Transport != "" {
			rows = append(rows, helpRow{command: "Transport", description: result.Transport})
		}
		if result.Destination != "" {
			rows = append(rows, helpRow{command: "Destination", description: result.Destination})
		}
		rows = append(rows, helpRow{command: "Vérifié", description: strconv.FormatBool(result.Verified)})
		if result.Note != "" {
			rows = append(rows, helpRow{command: "Information", description: result.Note})
		}
		printInformationBlock(output, "Résultat "+result.Server, rows)
	}
}

func printMenuMachines(output io.Writer, c *Config) {
	for index, id := range sortedKeys(c.Servers) {
		if index > 0 {
			fmt.Fprintln(output)
		}
		s := c.Servers[id]
		initial, timeout, interval := c.effectiveVerification(id)
		printInformationBlock(output, "Machine "+id+" — "+emptyAs(s.Name, "sans nom"), []helpRow{
			{command: "Adresse IP", description: s.IP},
			{command: "Adresse MAC", description: s.MAC},
			{command: "Réseau", description: s.Network},
			{command: "Active", description: strconv.FormatBool(enabled(s))},
			{command: "Contrôles", description: formatChecks(c.effectiveChecks(id))},
			{command: "Attente initiale", description: initial.String()},
			{command: "Délai maximal", description: timeout.String()},
			{command: "Intervalle", description: interval.String()},
		})
	}
}

func printMenuUPS(output io.Writer, statuses map[string]UPSStatus) {
	if len(statuses) == 0 {
		fmt.Fprintln(output, "Aucun UPS NUT configuré.")
		return
	}
	for index, id := range sortedKeys(statuses) {
		if index > 0 {
			fmt.Fprintln(output)
		}
		status := statuses[id]
		if status.Error != "" {
			printInformationBlock(output, "UPS "+id, []helpRow{{command: "Erreur", description: status.Error}})
			continue
		}
		battery := "inconnue"
		if status.BatteryKnown {
			battery = fmt.Sprintf("%.0f%%", status.Battery)
		}
		printInformationBlock(output, "UPS "+id, []helpRow{
			{command: "État", description: emptyAs(status.RawStatus, "inconnu")},
			{command: "Batterie", description: battery},
		})
	}
}

func printMenuAutomations(output io.Writer, plans []restorePlan, cycles []PowerCycle, jobs []Job) {
	if len(plans) == 0 {
		fmt.Fprintln(output, "Aucune politique de reprise NUT configurée.")
	}
	printed := false
	for _, plan := range plans {
		if printed {
			fmt.Fprintln(output)
		}
		printInformationBlock(output, "Politique du groupe "+plan.Group, []helpRow{
			{command: "UPS", description: plan.UPS},
			{command: "Mode", description: plan.Mode},
			{command: "Stabilité", description: plan.StableFor.String()},
			{command: "Fréquence", description: plan.PollInterval.String()},
			{command: "Échec", description: plan.FailurePolicy},
			{command: "Tentatives", description: strconv.Itoa(plan.RetryLimit)},
			{command: "Intervalle", description: plan.RetryInterval.String()},
		})
		printed = true
	}
	for _, cycle := range cycles {
		if printed {
			fmt.Fprintln(output)
		}
		printInformationBlock(output, "Cycle UPS "+cycle.UPS, []helpRow{
			{command: "État", description: cycle.State},
			{command: "Raison", description: emptyAs(cycle.Reason, "aucune")},
			{command: "Batterie", description: formatBattery(cycle)},
		})
		printed = true
	}
	for _, job := range jobs {
		if printed {
			fmt.Fprintln(output)
		}
		printInformationBlock(output, "Tâche "+job.ID, []helpRow{
			{command: "Type", description: job.Kind},
			{command: "Cible", description: job.Target},
			{command: "État", description: job.State},
		})
		printed = true
	}
}

func formatBattery(cycle PowerCycle) string {
	if !cycle.BatteryKnown {
		return "inconnue"
	}
	return fmt.Sprintf("%.0f%%", cycle.Battery)
}

func selectMenuServer(reader *bufio.Reader, output io.Writer, c *Config) (string, bool) {
	ids := sortedKeys(c.Servers)
	if len(ids) == 0 {
		fmt.Fprintln(output, "Aucune machine configurée.")
		return "", false
	}
	fmt.Fprintln(output, "Machines disponibles:")
	for i, id := range ids {
		fmt.Fprintf(output, "[%d] %s — %s\n", i+1, id, emptyAs(c.Servers[id].Name, "sans nom"))
	}
	for {
		value := prompt(reader, output, "Choisissez un numéro ou un identifiant (Entrée=annuler): ")
		if value == "?" || value == "aide" || value == "help" {
			printFieldHelp(output, "Choisissez un numéro ou un identifiant")
			continue
		}
		if value == "" || value == "annuler" || value == "retour" {
			return "", false
		}
		if index, err := strconv.Atoi(value); err == nil && index >= 1 && index <= len(ids) {
			return ids[index-1], true
		}
		if _, ok := c.Servers[value]; ok {
			return value, true
		}
		fmt.Fprintln(output, "Sélection inconnue ou ambiguë. Aucun paquet ne sera envoyé; recommencez ou appuyez sur Entrée.")
	}
}
func generateToken() (plain, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	plain = hex.EncodeToString(b)
	s := sha256.Sum256([]byte(plain))
	hash = hex.EncodeToString(s[:])
	return plain, hash, nil
}
func tokenCLI(c *Config, args []string) error {
	return tokenCLIWithIO(c, args, os.Stdin, os.Stdout)
}

func tokenCLIWithIO(c *Config, args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	if args[0] == "list" {
		if len(args) != 1 {
			return errUsage
		}
		f, err := readTokenFile(c.API.TokensFile)
		if err != nil {
			return err
		}
		for _, token := range f.Tokens {
			fmt.Fprintf(output, "%s\tpermissions=%s\tactif=%t\n", token.ID, strings.Join(token.Permissions, ","), !token.Disabled)
		}
		return nil
	}
	if args[0] == "revoke" {
		fs := flag.NewFlagSet("token revoke", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "identifiant")
		if fs.Parse(args[1:]) != nil || *id == "" {
			return errUsage
		}
		f, err := readTokenFile(c.API.TokensFile)
		if err != nil {
			return err
		}
		index := -1
		for i := range f.Tokens {
			if f.Tokens[i].ID == *id {
				index = i
			}
		}
		if index < 0 {
			return fmt.Errorf("jeton %q inconnu", *id)
		}
		if f.Tokens[index].Disabled {
			return fmt.Errorf("jeton %q déjà révoqué", *id)
		}
		if !confirmPrompt(bufio.NewReader(input), output, "Révoquer le jeton "+*id+" ?") {
			fmt.Fprintln(output, "Révocation annulée.")
			return nil
		}
		f.Tokens[index].Disabled = true
		if err := writeTokenFile(c.API.TokensFile, f); err != nil {
			return err
		}
		fmt.Fprintln(output, "Jeton révoqué:", *id)
		return nil
	}
	if args[0] != "create" {
		return errUsage
	}
	fs := flag.NewFlagSet("token create", flag.ContinueOnError)
	id := fs.String("id", "admin", "identifiant")
	perms := fs.String("permissions", "read,wake,admin", "permissions séparées par virgule")
	if fs.Parse(args[1:]) != nil {
		return errUsage
	}
	allowed := map[string]bool{"read": true, "wake": true, "admin": true}
	var ps []string
	for _, p := range strings.Split(*perms, ",") {
		p = strings.TrimSpace(p)
		if !allowed[p] {
			return fmt.Errorf("permission invalide %q", p)
		}
		ps = append(ps, p)
	}
	f, e := readTokenFile(c.API.TokensFile)
	if e != nil {
		return e
	}
	for _, t := range f.Tokens {
		if t.ID == *id {
			return fmt.Errorf("jeton %q existe déjà", *id)
		}
	}
	plain, hash, tokenErr := generateToken()
	if tokenErr != nil {
		return tokenErr
	}
	f.Tokens = append(f.Tokens, TokenRecord{ID: *id, Hash: hash, Permissions: ps})
	if e := writeTokenFile(c.API.TokensFile, f); e != nil {
		return e
	}
	fmt.Fprintln(output, "JETON (affiché une seule fois):", plain)
	return nil
}

func readTokenFile(path string) (tokenFile, error) {
	var f tokenFile
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return f, nil
	}
	return f, json.Unmarshal(b, &f)
}

func writeTokenFile(path string, f tokenFile) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("fichier de jetons refusé: lien symbolique")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tokens-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err = temp.Chmod(0600); err == nil {
		err = json.NewEncoder(temp).Encode(f)
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = secureTokenFile(tempPath); err != nil {
		return fmt.Errorf("sécurisation du fichier de jetons: %w", err)
	}
	if runtime.GOOS == "windows" {
		if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Rename(tempPath, path)
}
