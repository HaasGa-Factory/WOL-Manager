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
	"sort"
	"strings"
	"time"
)

var (
	version        = "1.1.1"
	sourceRevision = "unknown"
	buildChannel   = "unknown"
)

var errUsage = errors.New("usage invalide")

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "erreur:", err)
		if errors.Is(err, errUsage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
func run(args []string) error {
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
	if cmd == "config" && len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		return printConfigHelp(os.Stdout, args[0])
	}
	if cmd == "config" && len(args) > 0 && args[0] == "edit" {
		return editConfig(*configPath)
	}
	if cmd == "config" && len(args) == 1 {
		switch args[0] {
		case "network", "networks", "machine", "server", "servers", "group", "groups", "nut", "ups":
			return configSection(*configPath, args[0], os.Stdin, os.Stdout)
		}
	}
	c, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if cmd == "config" && len(args) > 0 && args[0] == "check" {
		return output(*jsonOut, map[string]any{"valid": true, "version": c.Version}, "configuration valide")
	}
	if cmd == "token" {
		return tokenCLI(c, args)
	}
	jobs, err := openJobs(c.Storage.StateFile)
	if err != nil {
		return err
	}
	audit := &Auditor{path: c.Storage.AuditFile}
	engine := &Engine{cfg: c, audit: audit}
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
		return output(*jsonOut, checkServer(context.Background(), c, args[0]), "")
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
	fmt.Fprintln(output, `WoL Manager `+version+`
Usage: wol [--config FILE] [--json] COMMAND
  setup                         assistant de configuration
  update [--check] [--channel main|dev]
                                vérifie ou installe une mise à jour
  completion bash               génère l'autocomplétion Bash
  list                          liste des serveurs
  status SERVER                 vérifie un serveur
  wake SERVER [--dry-run]       réveille un serveur et ses dépendances
  wake --group GROUP            réveille un groupe
  wake --all                    réveille tous les serveurs actifs
  config check                  valide la configuration
  config network|machine|group|nut
                                gestion guidée sans édition YAML
  config edit                   édition avancée manuelle
  token create                  crée un jeton API affiché une seule fois
  network test [SERVER]         explique les transports
  network discover [options]    découverte passive ou active
  ups status|test               état/diagnostic en lecture seule des UPS NUT
  jobs                          liste les tâches persistées
  menu                          interface interactive
  api serve                     démarre l'API
  relay serve|check             relais distant`)
}

func printConfigHelp(output io.Writer, section string) error {
	var details string
	switch section {
	case "network":
		details = `Actions: ajouter, modifier, supprimer ou revenir.
Paramètres demandés: identifiant, CIDR IPv4, interface, IP source et transports WoL.
Valeurs par défaut: interface et IP source automatiques; broadcast local proposé à la création.
Contraintes: identifiant unique, CIDR IPv4 valide; suppression refusée si une machine utilise le réseau.
Exemple: sudo wol config network`
	case "machine":
		details = `Actions: ajouter, modifier, supprimer ou revenir.
Paramètres demandés: identifiant, nom, MAC, IPv4, réseau, groupes, dépendances et vérification après réveil.
Valeurs par défaut: port WoL global; vérification ICMP/TCP/ARP facultative.
Contraintes: MAC et IPv4 valides, réseau existant; suppression refusée tant que la machine est référencée.
Exemple: sudo wol config machine`
	case "group":
		details = `Actions: ajouter, modifier, supprimer ou revenir.
Paramètres demandés: identifiant, priorité, machines et dépendances.
Valeurs par défaut: listes vides; priorité demandée par l'assistant.
Contraintes: références existantes; suppression refusée si le groupe est encore utilisé.
Exemple: sudo wol config group`
	case "nut":
		details = `Actions: détecter localement, ajouter ou modifier un serveur, sélectionner ou supprimer des UPS, tester ou reporter.
Paramètres demandés: adresse IP/DNS, port, TLS éventuel et UPS à surveiller.
Valeurs par défaut: 127.0.0.1:3493 pour la détection locale; port distant 3493; délai 3 s.
Contraintes: lecture seule, aucun scan réseau, aucune installation ni modification du serveur NUT.
Exemple: sudo wol config nut`
	default:
		return errUsage
	}
	fmt.Fprintf(output, `Usage: wol [--config FILE] config %s [--help]

Gestion guidée interactive de la section %s.
%s

Options:
  -h, --help       affiche cette aide et quitte sans charger la configuration
  --config FILE    option globale à placer avant "config" (défaut: /etc/wol-manager/wol.yaml)

Mode interactif: l'assistant demande les valeurs et une confirmation avant l'écriture atomique.
Mode non interactif: aucune modification n'est proposée; utilisez "wol config check" pour valider.
`, section, section, details)
	return nil
}

const bashCompletion = `# bash completion for WoL Manager
_wol() {
    local cur cmd i
    cur="${COMP_WORDS[COMP_CWORD]}"
    for ((i=1; i<COMP_CWORD; i++)); do
        case "${COMP_WORDS[i]}" in
            --config) ((i++)) ;;
            --json) ;;
            *) cmd="${COMP_WORDS[i]}"; break ;;
        esac
    done
    if [[ -z "$cmd" ]]; then
        COMPREPLY=( $(compgen -W "setup update list status wake config token network ups jobs menu api relay version completion --config --json" -- "$cur") )
        return
    fi
    case "$cmd" in
        update)
            if [[ "${COMP_WORDS[COMP_CWORD-1]}" == "--channel" ]]; then
                COMPREPLY=( $(compgen -W "main dev" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "--check --channel" -- "$cur") )
            fi ;;
        config) COMPREPLY=( $(compgen -W "check edit network machine group nut" -- "$cur") ) ;;
        wake) COMPREPLY=( $(compgen -W "--dry-run --group --all" -- "$cur") ) ;;
        network) COMPREPLY=( $(compgen -W "test discover" -- "$cur") ) ;;
        ups) COMPREPLY=( $(compgen -W "status test" -- "$cur") ) ;;
        token) COMPREPLY=( $(compgen -W "create" -- "$cur") ) ;;
        api) COMPREPLY=( $(compgen -W "serve" -- "$cur") ) ;;
        relay) COMPREPLY=( $(compgen -W "serve check" -- "$cur") ) ;;
        completion) COMPREPLY=( $(compgen -W "bash" -- "$cur") ) ;;
    esac
}
complete -F _wol wol
`

func wakeCLI(c *Config, e *Engine, args []string, jsonOut bool) error {
	args = flagsFirst(args, map[string]bool{"--group": true})
	fs := flag.NewFlagSet("wake", flag.ContinueOnError)
	group := fs.String("group", "", "groupe")
	all := fs.Bool("all", false, "tous")
	dry := fs.Bool("dry-run", false, "simulation")
	if er := fs.Parse(args); er != nil {
		return errUsage
	}
	e.dry = *dry
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
		ids := args[1:]
		if len(ids) == 0 {
			for id := range c.Servers {
				ids = append(ids, id)
			}
		}
		for _, id := range ids {
			p, e := transportPlan(c, id)
			out[id] = map[string]any{"plan": p, "error": errorString(e)}
		}
		return output(true, out, "")
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
			return errors.New("découverte active désactivée dans la configuration")
		}
		fmt.Fprintln(os.Stderr, "AVERTISSEMENT: diagnostic réseau actif limité aux sous-réseaux explicitement autorisés")
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
		return errUsage
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
			return errUsage
		}
		return serveRelay(c, e, *id, *listen, *cert, *key, *ca)
	case "check":
		if len(args) != 2 {
			return errUsage
		}
		r, ok := c.Relays[args[1]]
		if !ok {
			return fmt.Errorf("relais inconnu")
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
	fmt.Println(text)
	return nil
}
func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func editConfig(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
func menu(configPath string, c *Config, e *Engine, jsonOut bool) error {
	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\n1 Machines  2 Réseaux  3 Groupes  4 UPS  5 Valider  6 Configurer  7 Simulation WoL  8 Réveil réel  0 Quitter\n> ")
		x, _ := r.ReadString('\n')
		switch strings.TrimSpace(x) {
		case "0":
			return nil
		case "1":
			_ = output(jsonOut, c.Servers, "")
		case "2":
			_ = output(jsonOut, c.Networks, "")
		case "3":
			_ = output(jsonOut, c.Groups, "")
		case "4":
			out := map[string]UPSStatus{}
			for id, u := range c.UPS {
				out[id] = nutStatus(context.Background(), id, u)
			}
			_ = output(jsonOut, out, "")
		case "5":
			if err := c.Validate(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println("configuration valide")
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
			fmt.Print("Machine: ")
			id, _ := r.ReadString('\n')
			dryEngine := &Engine{cfg: c, audit: e.audit, dry: true, noWait: true}
			v, err := dryEngine.wakeTree(context.Background(), strings.TrimSpace(id), map[string]bool{})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				_ = output(jsonOut, v, "")
			}
		case "8":
			fmt.Print("Machine: ")
			id, _ := r.ReadString('\n')
			id = strings.TrimSpace(id)
			if !confirmPrompt(r, os.Stdout, "Envoyer réellement un paquet WoL à "+id+" ?") {
				fmt.Println("Réveil annulé.")
				continue
			}
			v, err := e.wakeTree(context.Background(), id, map[string]bool{})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				_ = output(jsonOut, v, "")
			}
		default:
			fmt.Println("Choix invalide.")
		}
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
	if len(args) == 0 || args[0] != "create" {
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
	var f tokenFile
	if b, e := os.ReadFile(c.API.TokensFile); e == nil {
		if len(bytes.TrimSpace(b)) > 0 {
			e = json.Unmarshal(b, &f)
		}
		if e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
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
	if e := os.MkdirAll(filepath.Dir(c.API.TokensFile), 0750); e != nil {
		return e
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	if e := os.WriteFile(c.API.TokensFile, b, 0600); e != nil {
		return e
	}
	if e := secureTokenFile(c.API.TokensFile); e != nil {
		return fmt.Errorf("sécurisation du fichier de jetons: %w", e)
	}
	fmt.Println("JETON (affiché une seule fois):", plain)
	return nil
}
