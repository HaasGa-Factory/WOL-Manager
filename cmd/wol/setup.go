package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func setup(path string) error {
	return setupWithIO(path, os.Stdin, os.Stdout)
}

func setupWithIO(path string, input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	session, err := newConfigSession(path)
	if err != nil {
		return err
	}
	if session.existing {
		fmt.Fprintf(output, "Configuration existante détectée: %s.\n", path)
	} else {
		fmt.Fprintln(output, "Création d'une nouvelle configuration guidée.")
		fmt.Fprintln(output, "NUT signifie Network UPS Tools. Il permet de lire l'état d'un onduleur; cette fonction est facultative et WoL Manager ne commande jamais l'onduleur.")
		fmt.Fprintln(output, "Si vous l'activez, préparez l'adresse IP/DNS, le port (3493), le choix TLS/STARTTLS et, seulement si nécessaires, les identifiants stockés dans des fichiers secrets.")
		fmt.Fprintln(output, "Souhaitez-vous configurer un serveur NUT ?")
		fmt.Fprintln(output, "[1] Oui\n[2] Non\n[3] Plus tard\n[?] Aide")
	chooseNUT:
		for {
			switch prompt(reader, output, "> ") {
			case "1":
				if configureNUT(reader, output, session.cfg) {
					session.changed = true
				}
				break chooseNUT
			case "2", "3":
				break chooseNUT
			case "?", "aide", "help":
				printContextHelp(output, "nut")
			default:
				fmt.Fprintln(output, "Choix invalide. Saisissez 1, 2 ou 3; aucune valeur n'a été modifiée.")
			}
		}
	}

	for {
		fmt.Fprintln(output, "\nConfiguration WoL Manager :")
		fmt.Fprintln(output, "[1] Réseaux WoL\n[2] Machines\n[3] Groupes et dépendances\n[4] Serveurs NUT et UPS\n[5] Résumé, validation et enregistrement\n[6] Annuler sans modifier\n[7] Délais globaux de vérification\n[8] Découverte réseau\n[?] Aide")
		switch prompt(reader, output, "> ") {
		case "1":
			session.changed = manageNetworks(reader, output, session.cfg) || session.changed
		case "2":
			session.changed = manageServers(reader, output, session.cfg) || session.changed
		case "3":
			session.changed = manageGroups(reader, output, session.cfg) || session.changed
		case "4":
			session.changed = configureNUT(reader, output, session.cfg) || session.changed
		case "5":
			showConfigSummary(output, session.cfg)
			if err := session.cfg.Validate(); err != nil {
				fmt.Fprintf(output, "Configuration invalide :\n%v\n", err)
				continue
			}
			if issues := readinessIssues(session.cfg); len(issues) > 0 {
				fmt.Fprintln(output, "PRÉPARATION IMPOSSIBLE: la syntaxe est valide, mais la configuration n'est pas opérationnelle:")
				for _, issue := range issues {
					fmt.Fprintln(output, "-", issue)
				}
				fmt.Fprintln(output, "Ouvrez Réseaux WoL, modifiez le réseau indiqué et activez le broadcast local, l'unicast ou un relais utilisable.")
				continue
			}
			if err := simulateAllMachines(output, session.cfg); err != nil {
				fmt.Fprintln(output, "Simulation finale échouée:", err)
				fmt.Fprintln(output, "Retournez dans Réseaux WoL ou Machines pour corriger la valeur indiquée; rien n'a été écrit.")
				continue
			}
			if !confirmPrompt(reader, output, "Enregistrer cette configuration ?") {
				fmt.Fprintln(output, "Aucune modification enregistrée.")
				continue
			}
			if err := writeManagedConfig(path, session.base, session.cfg, session.existing); err != nil {
				return err
			}
			fmt.Fprintln(output, "Configuration enregistrée et validée.")
			return offerDryRun(reader, output, session.cfg)
		case "6":
			fmt.Fprintln(output, "Configuration existante conservée sans modification.")
			return nil
		case "7":
			session.changed = configureGlobalVerification(reader, output, session.cfg) || session.changed
		case "8":
			session.changed = configureDiscovery(reader, output, session.cfg) || session.changed
		case "?", "aide", "help":
			printContextHelp(output, "setup")
		default:
			fmt.Fprintln(output, "Choix invalide.")
		}
	}
}

func configureNUT(reader *bufio.Reader, output io.Writer, cfg *Config) bool {
	fmt.Fprintln(output, "NUT (Network UPS Tools) lit un onduleur sans le commander. OL=secteur présent, OB=sur batterie, LB=batterie faible, FSD=arrêt forcé. L'identifiant du serveur est un nom local WoL Manager; l'identifiant UPS est fourni par NUT.")
	working := cfg.UPS
	before, _ := yaml.Marshal(working)
	if len(working) == 0 {
		fmt.Fprintln(output, "Recherche d'un serveur NUT local sur 127.0.0.1:3493...")
		local := UPSConfig{Address: "127.0.0.1:3493", Timeout: Duration(3 * time.Second)}
		devices, err := listNUTUPS(context.Background(), local)
		if err != nil {
			fmt.Fprintf(output, "Aucun serveur NUT local utilisable: %v\n", err)
		} else if len(devices) == 0 {
			fmt.Fprintln(output, "Serveur NUT local détecté, mais aucun UPS n'est disponible.")
		} else {
			fmt.Fprintln(output, "Serveur NUT local détecté.")
			if err := selectNUTDevices(reader, output, "local", local, devices, working); err != nil {
				fmt.Fprintf(output, "Sélection locale non enregistrée: %v\n", err)
			}
		}
	}

	for {
		fmt.Fprintln(output, "\nConfiguration NUT :")
		fmt.Fprintln(output, "[1] Ajouter ou modifier un serveur\n[2] Supprimer un UPS configuré\n[3] Tester les UPS configurés\n[4] Terminer\n[5] Annuler les changements NUT\n[?] Aide")
		switch prompt(reader, output, "> ") {
		case "1":
			if err := configureRemoteNUT(reader, output, working); err != nil {
				fmt.Fprintf(output, "Configuration reportée: %v\n", err)
			}
		case "2":
			removeConfiguredUPS(reader, output, cfg)
		case "3":
			testConfiguredUPS(output, working)
		case "4":
			after, _ := yaml.Marshal(working)
			return !bytes.Equal(before, after)
		case "5":
			var original map[string]UPSConfig
			_ = yaml.Unmarshal(before, &original)
			for id := range working {
				delete(working, id)
			}
			for id, ups := range original {
				working[id] = ups
			}
			return false
		case "?", "aide", "help":
			printContextHelp(output, "nut")
		default:
			fmt.Fprintln(output, "Choix invalide.")
		}
	}
}

type configSession struct {
	base     []byte
	cfg      *Config
	existing bool
	changed  bool
}

func newConfigSession(path string) (*configSession, error) {
	base := []byte(minimalConfig)
	existing := false
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("configuration refusée: la cible est un lien symbolique")
		}
		existing = true
		base, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	cfg, err := loadConfigData(base)
	if err != nil {
		if existing {
			return nil, fmt.Errorf("configuration existante invalide, aucune modification: %w", err)
		}
		return nil, err
	}
	if cfg.Networks == nil {
		cfg.Networks = map[string]Network{}
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]Server{}
	}
	if cfg.Groups == nil {
		cfg.Groups = map[string]Group{}
	}
	if cfg.UPS == nil {
		cfg.UPS = map[string]UPSConfig{}
	}
	return &configSession{base: base, cfg: cfg, existing: existing}, nil
}

func manageNetworks(reader *bufio.Reader, output io.Writer, cfg *Config) bool {
	changed := false
	for {
		fmt.Fprintln(output, "\nRéseaux WoL :")
		printNetworks(output, cfg.Networks)
		fmt.Fprintln(output, "[1] Ajouter\n[2] Modifier\n[3] Supprimer\n[4] Retour\n[?] Aide")
		switch prompt(reader, output, "> ") {
		case "1":
			if addOrEditNetwork(reader, output, cfg, "") {
				changed = true
			}
		case "2":
			if addOrEditNetwork(reader, output, cfg, prompt(reader, output, "Identifiant du réseau à modifier: ")) {
				changed = true
			}
		case "3":
			id := prompt(reader, output, "Identifiant du réseau à supprimer: ")
			if _, ok := cfg.Networks[id]; !ok {
				fmt.Fprintln(output, "Réseau inconnu.")
				continue
			}
			used := false
			for serverID, server := range cfg.Servers {
				if server.Network == id {
					fmt.Fprintf(output, "Suppression refusée: réseau utilisé par la machine %s.\n", serverID)
					used = true
				}
			}
			if !used && confirmPrompt(reader, output, "Supprimer le réseau "+id+" ?") {
				delete(cfg.Networks, id)
				changed = true
			}
		case "4":
			return changed
		case "?", "aide", "help":
			printContextHelp(output, "network")
		default:
			fmt.Fprintln(output, "Choix invalide.")
		}
	}
}

func addOrEditNetwork(reader *bufio.Reader, output io.Writer, cfg *Config, id string) bool {
	old, editing := cfg.Networks[id]
	if id == "" {
		var ok bool
		id, ok = promptNewID(reader, output, "réseau", cfg.Networks)
		if !ok {
			return false
		}
	} else if !editing {
		fmt.Fprintln(output, "Réseau inconnu.")
		return false
	}
	interfaces := detectIPv4Interfaces()
	if len(interfaces) > 0 {
		fmt.Fprintln(output, "Interfaces IPv4 détectées :")
		for _, item := range interfaces {
			fmt.Fprintf(output, "- %s %s (réseau %s)\n", item.Name, item.Address, item.CIDR)
		}
	}
	n := old
	if !editing && len(interfaces) == 1 {
		if n.CIDR == "" {
			n.CIDR = interfaces[0].CIDR
		}
		if n.Interface == "" {
			n.Interface = interfaces[0].Name
		}
		if n.SourceIP == "" {
			n.SourceIP = interfaces[0].Address
		}
		fmt.Fprintln(output, "Une seule interface IPv4 a été détectée: ses valeurs sont proposées. Appuyez sur Entrée pour les accepter.")
	}
	for {
		n.CIDR = promptDefault(reader, output, "Réseau CIDR", n.CIDR)
		if _, err := parseIPv4CIDR(n.CIDR); err == nil {
			break
		} else {
			fmt.Fprintln(output, err, "Recommencez.")
		}
	}
	n.Interface = promptDefault(reader, output, "Interface Linux (facultatif)", n.Interface)
	for {
		n.SourceIP = promptDefault(reader, output, "Adresse IPv4 source (facultatif)", n.SourceIP)
		if n.SourceIP == "" || net.ParseIP(n.SourceIP).To4() != nil {
			break
		}
		fmt.Fprintln(output, "Adresse IPv4 invalide. Recommencez.")
	}
	fmt.Fprintln(output, "Transports: broadcast local (recommandé sur le même réseau), unicast (direct mais dépend d'ARP), broadcast dirigé (routé et plus risqué), relais (recommandé pour un réseau distant).")
	n.AllowLocalBroadcast = promptBool(reader, output, "Autoriser le broadcast local", valueOrDefault(n.AllowLocalBroadcast, !editing))
	n.AllowUnicast = promptBool(reader, output, "Autoriser l'unicast", valueOrDefault(n.AllowUnicast, !editing))
	n.AllowDirectedBroadcast = promptBool(reader, output, "Autoriser le broadcast dirigé", n.AllowDirectedBroadcast)
	if len(cfg.Relays) > 0 {
		fmt.Fprintln(output, "Relais disponibles:", strings.Join(sortedKeys(cfg.Relays), ", "))
		for {
			n.Relay = promptDefault(reader, output, "Identifiant du relais (facultatif)", n.Relay)
			if n.Relay == "" {
				break
			}
			if _, ok := cfg.Relays[n.Relay]; ok {
				break
			}
			fmt.Fprintln(output, "Relais inconnu. Recommencez ou saisissez - pour vider.")
		}
	}
	cfg.Networks[id] = n
	if err := cfg.Validate(); err != nil {
		if editing {
			cfg.Networks[id] = old
		} else {
			delete(cfg.Networks, id)
		}
		fmt.Fprintf(output, "Réseau refusé: %v\n", err)
		return false
	}
	fmt.Fprintf(output, "Réseau %s prêt. Transports activés: %s.\n", id, strings.Join(enabledTransports(n), ", "))
	return true
}

func manageServers(reader *bufio.Reader, output io.Writer, cfg *Config) bool {
	changed := false
	for {
		fmt.Fprintln(output, "\nMachines :")
		printServers(output, cfg.Servers)
		fmt.Fprintln(output, "[1] Ajouter\n[2] Modifier\n[3] Supprimer\n[4] Retour\n[?] Aide")
		switch prompt(reader, output, "> ") {
		case "1":
			if addOrEditServer(reader, output, cfg, "") {
				changed = true
			}
		case "2":
			if addOrEditServer(reader, output, cfg, prompt(reader, output, "Identifiant de la machine à modifier: ")) {
				changed = true
			}
		case "3":
			id := prompt(reader, output, "Identifiant de la machine à supprimer: ")
			if _, ok := cfg.Servers[id]; !ok {
				fmt.Fprintln(output, "Machine inconnue.")
				continue
			}
			if reason := serverDeleteBlocker(cfg, id); reason != "" {
				fmt.Fprintln(output, "Suppression refusée:", reason)
				continue
			}
			if confirmPrompt(reader, output, "Supprimer la machine "+id+" ?") {
				delete(cfg.Servers, id)
				changed = true
			}
		case "4":
			return changed
		case "?", "aide", "help":
			printContextHelp(output, "machine")
		default:
			fmt.Fprintln(output, "Choix invalide.")
		}
	}
}

func addOrEditServer(reader *bufio.Reader, output io.Writer, cfg *Config, id string) bool {
	if len(cfg.Networks) == 0 {
		fmt.Fprintln(output, "Ajoutez d'abord un réseau WoL.")
		return false
	}
	old, editing := cfg.Servers[id]
	if id == "" {
		var ok bool
		id, ok = promptNewID(reader, output, "machine", cfg.Servers)
		if !ok {
			return false
		}
	} else if !editing {
		fmt.Fprintln(output, "Machine inconnue.")
		return false
	}
	s := old
	s.Name = promptDefault(reader, output, "Nom", s.Name)
	for {
		s.MAC = promptDefault(reader, output, "Adresse MAC", s.MAC)
		if _, err := net.ParseMAC(s.MAC); err == nil {
			break
		}
		fmt.Fprintln(output, "Adresse MAC invalide (ex. 02:00:00:00:00:01). Recommencez.")
	}
	for {
		s.IP = promptDefault(reader, output, "Adresse IPv4", s.IP)
		if net.ParseIP(s.IP).To4() != nil {
			break
		}
		fmt.Fprintln(output, "Adresse IPv4 invalide. Recommencez.")
	}
	fmt.Fprintln(output, "Réseaux disponibles:", strings.Join(sortedKeys(cfg.Networks), ", "))
	for {
		s.Network = promptDefault(reader, output, "Réseau", s.Network)
		if _, ok := cfg.Networks[s.Network]; ok {
			break
		}
		fmt.Fprintln(output, "Réseau inconnu. Recommencez.")
	}
	s.Groups = promptKnownIDs(reader, output, "Groupes (séparés par des virgules, facultatif)", s.Groups, cfg.Groups)
	s.Dependencies = promptKnownIDs(reader, output, "Dépendances machines (virgules, facultatif)", s.Dependencies, cfg.Servers)
	for _, dep := range s.Dependencies {
		if dep == id {
			fmt.Fprintln(output, "Une machine ne peut pas dépendre d'elle-même.")
			return false
		}
	}
	s.Checks = promptChecks(reader, output, s.Checks)
	s.CheckLogic = promptCheckLogic(reader, output, s.CheckLogic)
	verification, err := promptVerification(reader, output, s.Verification)
	if err != nil {
		fmt.Fprintf(output, "Délais de vérification refusés: %v\n", err)
		return false
	}
	s.Verification = verification
	cfg.Servers[id] = s
	if err := cfg.Validate(); err != nil {
		if editing {
			cfg.Servers[id] = old
		} else {
			delete(cfg.Servers, id)
		}
		fmt.Fprintf(output, "Machine refusée: %v\n", err)
		return false
	}
	fmt.Fprintf(output, "Machine %s prête.\n", id)
	return true
}

func manageGroups(reader *bufio.Reader, output io.Writer, cfg *Config) bool {
	changed := false
	for {
		fmt.Fprintln(output, "\nGroupes :")
		printGroups(output, cfg.Groups)
		fmt.Fprintln(output, "[1] Ajouter\n[2] Modifier\n[3] Supprimer\n[4] Retour\n[?] Aide")
		switch prompt(reader, output, "> ") {
		case "1":
			if addOrEditGroup(reader, output, cfg, "") {
				changed = true
			}
		case "2":
			if addOrEditGroup(reader, output, cfg, prompt(reader, output, "Identifiant du groupe à modifier: ")) {
				changed = true
			}
		case "3":
			id := prompt(reader, output, "Identifiant du groupe à supprimer: ")
			if _, ok := cfg.Groups[id]; !ok {
				fmt.Fprintln(output, "Groupe inconnu.")
				continue
			}
			if reason := groupDeleteBlocker(cfg, id); reason != "" {
				fmt.Fprintln(output, "Suppression refusée:", reason)
				continue
			}
			if confirmPrompt(reader, output, "Supprimer le groupe "+id+" ?") {
				delete(cfg.Groups, id)
				changed = true
			}
		case "4":
			return changed
		case "?", "aide", "help":
			printContextHelp(output, "group")
		default:
			fmt.Fprintln(output, "Choix invalide.")
		}
	}
}

func addOrEditGroup(reader *bufio.Reader, output io.Writer, cfg *Config, id string) bool {
	old, editing := cfg.Groups[id]
	if id == "" {
		var ok bool
		id, ok = promptNewID(reader, output, "groupe", cfg.Groups)
		if !ok {
			return false
		}
	} else if !editing {
		fmt.Fprintln(output, "Groupe inconnu.")
		return false
	}
	g := old
	for {
		priority := promptDefault(reader, output, "Priorité", strconv.Itoa(g.Priority))
		value, err := strconv.Atoi(priority)
		if err == nil && value >= 0 {
			duplicate := false
			for otherID, other := range cfg.Groups {
				if otherID != id && other.Priority == value {
					duplicate = true
				}
			}
			if !duplicate {
				g.Priority = value
				break
			}
			fmt.Fprintln(output, "Cette priorité est déjà utilisée. Une valeur plus petite démarre plus tôt.")
		} else {
			fmt.Fprintln(output, "Priorité invalide. Saisissez un nombre positif.")
		}
	}
	g.Servers = promptKnownIDs(reader, output, "Machines du groupe (virgules, facultatif)", g.Servers, cfg.Servers)
	g.Dependencies = promptKnownIDs(reader, output, "Dépendances machines (virgules, facultatif)", g.Dependencies, cfg.Servers)
	g.PowerRestore = promptPowerRestore(reader, output, cfg, g)
	cfg.Groups[id] = g
	if err := cfg.Validate(); err != nil {
		if editing {
			cfg.Groups[id] = old
		} else {
			delete(cfg.Groups, id)
		}
		fmt.Fprintf(output, "Groupe refusé: %v\n", err)
		return false
	}
	fmt.Fprintf(output, "Groupe %s prêt.\n", id)
	return true
}

func configSection(path, section string, input io.Reader, output io.Writer) error {
	session, err := newConfigSession(path)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(input)
	var changed bool
	switch section {
	case "network", "networks":
		changed = manageNetworks(reader, output, session.cfg)
	case "machine", "server", "servers":
		changed = manageServers(reader, output, session.cfg)
	case "group", "groups":
		changed = manageGroups(reader, output, session.cfg)
	case "nut", "ups":
		changed = configureNUT(reader, output, session.cfg)
	case "discovery":
		changed = configureDiscovery(reader, output, session.cfg)
	default:
		return errUsage
	}
	if !changed {
		fmt.Fprintln(output, "Aucune modification à enregistrer.")
		return nil
	}
	showConfigSummary(output, session.cfg)
	if err := session.cfg.Validate(); err != nil {
		return fmt.Errorf("configuration refusée: %w", err)
	}
	if !confirmPrompt(reader, output, "Enregistrer ces changements ?") {
		fmt.Fprintln(output, "Aucune modification enregistrée.")
		return nil
	}
	return writeManagedConfig(path, session.base, session.cfg, session.existing)
}

func configureDiscovery(reader *bufio.Reader, output io.Writer, cfg *Config) bool {
	old := cfg.Discovery
	before, _ := yaml.Marshal(old)
	fmt.Fprintln(output, "La découverte active exige deux verrous: activation globale ici et autorisation ponctuelle --allow-active-scan. Elle reste limitée aux sous-réseaux, au nombre de paquets, au débit et au timeout configurés.")
	cfg.Discovery.PassiveEnabled = promptBool(reader, output, "Activer la découverte passive", cfg.Discovery.PassiveEnabled)
	cfg.Discovery.ActiveEnabled = promptBool(reader, output, "Activer la découverte active", cfg.Discovery.ActiveEnabled)
	if cfg.Discovery.ActiveEnabled {
		for {
			value := promptDefault(reader, output, "Sous-réseaux autorisés (CIDR séparés par des virgules)", strings.Join(cfg.Discovery.AllowedSubnets, ","))
			var subnets []string
			valid := true
			for _, item := range strings.Split(value, ",") {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				if _, err := parseIPv4CIDR(item); err != nil {
					fmt.Fprintln(output, "CIDR invalide:", item)
					valid = false
					break
				}
				subnets = append(subnets, item)
			}
			if valid && len(subnets) > 0 {
				cfg.Discovery.AllowedSubnets = subnets
				break
			}
			fmt.Fprintln(output, "Au moins un sous-réseau IPv4 autorisé est requis.")
		}
		cfg.Discovery.Safeguards.RequireConfirmation = true
		for {
			value, err := strconv.Atoi(promptDefault(reader, output, "Limite maximale de paquets", strconv.Itoa(cfg.Discovery.Safeguards.MaxPackets)))
			if err == nil && value > 0 {
				cfg.Discovery.Safeguards.MaxPackets = value
				break
			}
			fmt.Fprintln(output, "Nombre invalide; saisissez un entier positif.")
		}
		for {
			value, err := strconv.ParseFloat(promptDefault(reader, output, "Débit maximal (paquets/seconde)", strconv.FormatFloat(cfg.Discovery.Safeguards.RatePPS, 'f', -1, 64)), 64)
			if err == nil && value > 0 {
				cfg.Discovery.Safeguards.RatePPS = value
				break
			}
			fmt.Fprintln(output, "Débit invalide; saisissez un nombre positif.")
		}
		for {
			raw := promptDefault(reader, output, "Timeout de découverte", cfg.Discovery.Safeguards.Timeout.Value(30*time.Second).String())
			value, err := time.ParseDuration(raw)
			if err == nil && value > 0 && value <= 24*time.Hour {
				cfg.Discovery.Safeguards.Timeout = Duration(value)
				break
			}
			fmt.Fprintln(output, "Durée invalide. Utilisez 30s, 2m ou 1m30s.")
		}
	}
	cfg.API.ActiveDiscovery = promptBool(reader, output, "Autoriser aussi la découverte active via l'API", cfg.API.ActiveDiscovery)
	if err := cfg.Validate(); err != nil {
		cfg.Discovery = old
		fmt.Fprintln(output, "Configuration de découverte refusée:", err)
		return false
	}
	after, _ := yaml.Marshal(cfg.Discovery)
	return !bytes.Equal(before, after)
}

func offerDryRun(reader *bufio.Reader, output io.Writer, cfg *Config) error {
	if len(cfg.Servers) == 0 || !confirmPrompt(reader, output, "Tester une machine en simulation WoL ?") {
		return nil
	}
	printServers(output, cfg.Servers)
	id := prompt(reader, output, "Machine à simuler: ")
	if _, ok := cfg.Servers[id]; !ok {
		return fmt.Errorf("machine inconnue %q", id)
	}
	engine := &Engine{cfg: cfg, audit: &Auditor{}, dry: true, noWait: true}
	result, err := engine.wakeTree(context.Background(), id, map[string]bool{})
	if err != nil {
		return err
	}
	for _, item := range result {
		fmt.Fprintf(output, "%s: %s vers %s (%s)\n", item.Server, item.Transport, item.Destination, item.Packet)
	}
	return nil
}

func showConfigSummary(output io.Writer, cfg *Config) {
	fmt.Fprintf(output, "\nRésumé: %d réseau(x), %d machine(s), %d groupe(s), %d UPS.\n", len(cfg.Networks), len(cfg.Servers), len(cfg.Groups), len(cfg.UPS))
	printNetworks(output, cfg.Networks)
	for _, id := range sortedKeys(cfg.Servers) {
		s := cfg.Servers[id]
		initial, timeout, interval := cfg.effectiveVerification(id)
		attempts, retries := cfg.effectiveVerificationAttempts(id)
		checks := formatChecks(cfg.effectiveChecks(id))
		selected, destination := "aucun", "indisponible"
		if plan, _ := transportPlan(cfg, id); len(plan) > 0 {
			for _, d := range plan {
				if d.State == "selected" {
					selected, destination = d.Name, emptyAs(d.Destination, "relais "+d.Relay)
				}
			}
		}
		ups := "aucun"
		if p := cfg.effectiveUPS(id); p != nil {
			ups = p.UPS
		}
		fmt.Fprintf(output, "- machine %s (%s): active=%t ip=%s mac=%s réseau=%s transport=%s destination=%s contrôles=%s logique=%s délais(initial=%s timeout=%s interval=%s tentatives=%d nouveaux_paquets=%d) dépendances=%s UPS=%s\n", id, emptyAs(s.Name, "sans nom"), enabled(s), s.IP, s.MAC, s.Network, selected, destination, checks, cfg.effectiveCheckLogic(id), initial, timeout, interval, attempts, retries, emptyAs(strings.Join(cfg.dependencies(id), ","), "aucune"), ups)
	}
	printGroups(output, cfg.Groups)
	for _, id := range sortedKeys(cfg.UPS) {
		u := cfg.UPS[id]
		fmt.Fprintf(output, "- UPS %s: serveur=%s identifiant_NUT=%s TLS=%t\n", id, u.Address, u.Name, u.TLS)
	}
}

func printNetworks(output io.Writer, networks map[string]Network) {
	for _, id := range sortedKeys(networks) {
		n := networks[id]
		fmt.Fprintf(output, "- réseau %s: %s interface=%s source=%s transports=%s port=%d\n", id, n.CIDR, emptyAs(n.Interface, "auto"), emptyAs(n.SourceIP, "auto"), strings.Join(enabledTransports(n), ","), n.Port)
	}
}

func printServers(output io.Writer, servers map[string]Server) {
	for _, id := range sortedKeys(servers) {
		s := servers[id]
		fmt.Fprintf(output, "- %s: %s %s réseau=%s\n", id, s.IP, s.MAC, s.Network)
	}
}

func printGroups(output io.Writer, groups map[string]Group) {
	for _, id := range sortedKeys(groups) {
		g := groups[id]
		fmt.Fprintf(output, "- groupe %s: priorité=%d machines=%s dépendances=%s", id, g.Priority, emptyAs(strings.Join(g.Servers, ","), "aucune"), emptyAs(strings.Join(g.Dependencies, ","), "aucune"))
		if p := g.PowerRestore; p != nil {
			fmt.Fprintf(output, " reprise_NUT(active=%t UPS=%s seuils=%s/%s stabilité=%s fréquence=%s mode=%s ordre=%s échec=%s retries=%d/%s)", p.Enabled, p.UPS, percent(p.LowBattery), percent(p.HighBattery), p.StableFor.Value(30*time.Second), p.PollInterval.Value(5*time.Second), defaultString(p.Mode, "sequential"), emptyAs(strings.Join(p.Order, ","), "groupe"), defaultString(p.FailurePolicy, "stop"), p.RetryLimit, p.RetryInterval.Value(10*time.Second))
		}
		fmt.Fprintln(output)
	}
}

func percent(value *int) string {
	if value == nil {
		return "hérité"
	}
	return fmt.Sprintf("%d%%", *value)
}

func formatChecks(checks []Check) string {
	if len(checks) == 0 {
		return "aucun (envoi sans confirmation)"
	}
	items := make([]string, 0, len(checks))
	for _, check := range checks {
		item := check.Type
		if check.Type == "tcp" {
			item = fmt.Sprintf("tcp:%d", check.Port)
		}
		items = append(items, item)
	}
	return strings.Join(items, ",")
}

func readinessIssues(cfg *Config) []string {
	var issues []string
	for _, id := range sortedKeys(cfg.Servers) {
		if !enabled(cfg.Servers[id]) {
			continue
		}
		if _, err := transportPlan(cfg, id); err != nil {
			issues = append(issues, fmt.Sprintf("machine %s (réseau %s): %v", id, cfg.Servers[id].Network, err))
		}
	}
	return issues
}

func simulateAllMachines(output io.Writer, cfg *Config) error {
	engine := &Engine{cfg: cfg, audit: &Auditor{}, dry: true, noWait: true}
	for _, id := range sortedKeys(cfg.Servers) {
		if !enabled(cfg.Servers[id]) {
			continue
		}
		if _, err := engine.wakeTree(context.Background(), id, map[string]bool{}); err != nil {
			return fmt.Errorf("machine %s: %w", id, err)
		}
		fmt.Fprintf(output, "Simulation WoL automatique réussie pour %s (aucun paquet envoyé).\n", id)
	}
	return nil
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type ipv4Interface struct{ Name, Address, CIDR string }

var detectIPv4Interfaces = systemIPv4Interfaces

func systemIPv4Interfaces() []ipv4Interface {
	var result []ipv4Interface
	interfaces, _ := net.Interfaces()
	for _, item := range interfaces {
		addresses, _ := item.Addrs()
		for _, address := range addresses {
			ip, network, err := net.ParseCIDR(address.String())
			if err == nil && ip.To4() != nil && !ip.IsLoopback() {
				result = append(result, ipv4Interface{Name: item.Name, Address: ip.String(), CIDR: network.String()})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name+result[i].Address < result[j].Name+result[j].Address })
	return result
}

func ipv4Interfaces() []string {
	items := detectIPv4Interfaces()
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Name+" "+item.Address+" (réseau "+item.CIDR+")")
	}
	return result
}

func promptNewID[T any](reader *bufio.Reader, output io.Writer, kind string, existing map[string]T) (string, bool) {
	fmt.Fprintln(output, "Saisissez « annuler » ou « retour » pour abandonner cet objet sans perdre les autres changements.")
	for {
		id := prompt(reader, output, "Identifiant du "+kind+": ")
		if id == "annuler" || id == "retour" {
			return "", false
		}
		if !validID(id) {
			fmt.Fprintln(output, "Identifiant invalide: utilisez lettres, chiffres, - ou _, sans espace (ex. truenas, pc-bureau, maison).")
			continue
		}
		if _, exists := existing[id]; exists {
			fmt.Fprintln(output, "Cet identifiant existe déjà. Recommencez.")
			continue
		}
		return id, true
	}
}

func enabledTransports(n Network) []string {
	var out []string
	if n.AllowLocalBroadcast {
		out = append(out, "broadcast local")
	}
	if n.AllowUnicast {
		out = append(out, "unicast")
	}
	if n.AllowDirectedBroadcast {
		out = append(out, "broadcast dirigé")
	}
	if n.Relay != "" {
		out = append(out, "relais "+n.Relay)
	}
	if len(out) == 0 {
		return []string{"aucun"}
	}
	return out
}

func promptDefault(reader *bufio.Reader, output io.Writer, label, current string) string {
	if current == "" {
		return prompt(reader, output, label+": ")
	}
	value := prompt(reader, output, fmt.Sprintf("%s [%s]: ", label, current))
	if value == "" {
		return current
	}
	if value == "-" {
		return ""
	}
	return value
}

func promptBool(reader *bufio.Reader, output io.Writer, label string, current bool) bool {
	for {
		def := "N"
		if current {
			def = "O"
		}
		value := strings.ToLower(prompt(reader, output, fmt.Sprintf("%s [o/n, défaut %s]: ", label, def)))
		switch value {
		case "":
			return current
		case "o", "oui", "y", "yes":
			return true
		case "n", "non", "no":
			return false
		default:
			fmt.Fprintln(output, "Réponse invalide: saisissez o ou n. La valeur n'a pas été modifiée.")
		}
	}
}

func confirmPrompt(reader *bufio.Reader, output io.Writer, label string) bool {
	for {
		value := strings.ToLower(prompt(reader, output, label+" [o/N]: "))
		switch value {
		case "", "n", "non", "no":
			return false
		case "o", "oui", "y", "yes":
			return true
		default:
			fmt.Fprintln(output, "Réponse invalide: saisissez o ou n.")
		}
	}
}

func promptKnownIDs[T any](reader *bufio.Reader, output io.Writer, label string, current []string, known map[string]T) []string {
	if len(known) > 0 {
		fmt.Fprintln(output, "Valeurs disponibles:", strings.Join(sortedKeys(known), ", "))
	}
	for {
		value := promptDefault(reader, output, label+" (- pour vider)", strings.Join(current, ","))
		if strings.TrimSpace(value) == "" {
			return nil
		}
		var result, unknown []string
		seen := map[string]bool{}
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item == "" || seen[item] {
				continue
			}
			if _, ok := known[item]; !ok {
				unknown = append(unknown, item)
				continue
			}
			result = append(result, item)
			seen[item] = true
		}
		if len(unknown) == 0 {
			return result
		}
		fmt.Fprintln(output, "Identifiant(s) inconnu(s):", strings.Join(unknown, ", "), "— recommencez.")
	}
}

func promptChecks(reader *bufio.Reader, output io.Writer, current []Check) []Check {
	currentText := "aucune"
	if len(current) > 0 {
		currentText = current[0].Type
	}
	fmt.Fprintf(output, "Vérification après réveil actuelle: %s\n", currentText)
	fmt.Fprintln(output, "ICMP: ping simple mais parfois bloqué. TCP: vérifie un port ouvert (22/80/443). ARP: présence locale, preuve plus faible. Aucune: paquet envoyé sans confirmation de démarrage.")
	for {
		fmt.Fprintln(output, "[1] ICMP\n[2] TCP\n[3] ARP\n[4] Aucune\n[5] Plusieurs contrôles\n[Entrée] Conserver\n[?] Aide")
		switch prompt(reader, output, "> ") {
		case "1":
			return []Check{{Type: "icmp", Timeout: Duration(3 * time.Second)}}
		case "2":
			for {
				port, err := strconv.Atoi(promptDefault(reader, output, "Port TCP (ex. 22, 80, 443)", "22"))
				if err == nil && port >= 1 && port <= 65535 {
					return []Check{{Type: "tcp", Port: port, Timeout: Duration(3 * time.Second)}}
				}
				fmt.Fprintln(output, "Port invalide (1 à 65535). Recommencez.")
			}
		case "3":
			return []Check{{Type: "arp", Timeout: Duration(3 * time.Second)}}
		case "4":
			return nil
		case "5":
			for {
				value := prompt(reader, output, "Contrôles (ex. icmp,tcp:22,tcp:443,arp): ")
				checks, err := parseChecks(value)
				if err == nil {
					return checks
				}
				fmt.Fprintln(output, "Contrôles invalides:", err, "Recommencez.")
			}
		case "?", "aide", "help":
			fmt.Fprintln(output, fieldHelp("contrôles"))
		case "":
			return current
		default:
			fmt.Fprintln(output, "Choix invalide. Recommencez.")
		}
	}
}

func parseChecks(value string) ([]Check, error) {
	var checks []Check
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(strings.ToLower(item))
		switch {
		case item == "icmp":
			checks = append(checks, Check{Type: "icmp", Timeout: Duration(3 * time.Second)})
		case item == "arp":
			checks = append(checks, Check{Type: "arp", Timeout: Duration(3 * time.Second)})
		case strings.HasPrefix(item, "tcp:"):
			port, err := strconv.Atoi(strings.TrimPrefix(item, "tcp:"))
			if err != nil || port < 1 || port > 65535 {
				return nil, fmt.Errorf("port TCP invalide %q", item)
			}
			checks = append(checks, Check{Type: "tcp", Port: port, Timeout: Duration(3 * time.Second)})
		default:
			return nil, fmt.Errorf("contrôle inconnu %q", item)
		}
	}
	if len(checks) == 0 {
		return nil, errors.New("aucun contrôle")
	}
	return checks, nil
}

func promptCheckLogic(reader *bufio.Reader, output io.Writer, current string) string {
	if current == "" {
		current = "any"
	}
	for {
		value := strings.ToLower(promptDefault(reader, output, "Logique des contrôles (any=OU: un succès suffit; all=ET: tous doivent réussir)", current))
		if value == "any" || value == "all" {
			return value
		}
		fmt.Fprintln(output, "Logique invalide: saisissez any ou all. Recommencez.")
	}
}

func promptPowerRestore(reader *bufio.Reader, output io.Writer, cfg *Config, group Group) *PowerRestorePolicy {
	if len(cfg.UPS) == 0 {
		fmt.Fprintln(output, "Reprise automatique indisponible: aucun UPS NUT configuré.")
		return group.PowerRestore
	}
	enabled := group.PowerRestore != nil && group.PowerRestore.Enabled
	if !promptBool(reader, output, "Activer la reprise automatique NUT pour ce groupe ?", enabled) {
		return nil
	}
	fmt.Fprintln(output, "Aide: le seuil bas réarme un cycle observé; le seuil haut et la stabilité secteur autorisent le démarrage. Une mesure inconnue ou un état OB/LB/FSD bloque tout nouveau réveil.")
	fmt.Fprintln(output, "Le mode sequential attend chaque machine et ses dépendances; parallel autorise les machines indépendantes sans contourner leurs prérequis.")
	policy := PowerRestorePolicy{Enabled: true, Mode: "sequential", FailurePolicy: "stop"}
	if group.PowerRestore != nil {
		policy = *group.PowerRestore
		policy.Enabled = true
	}
	fmt.Fprintln(output, "UPS disponibles:", strings.Join(sortedKeys(cfg.UPS), ", "))
	for {
		policy.UPS = promptDefault(reader, output, "UPS", policy.UPS)
		if _, ok := cfg.UPS[policy.UPS]; ok {
			break
		}
		fmt.Fprintln(output, "UPS inconnu. Recommencez.")
	}
	fmt.Fprintln(output, "Seuils batterie: [1] 40/70 %  [2] 20/80 %  [3] personnalisés")
	low, high := 40, 70
	if policy.LowBattery != nil && policy.HighBattery != nil {
		low, high = *policy.LowBattery, *policy.HighBattery
	}
	thresholdsReady := false
	for !thresholdsReady {
		switch prompt(reader, output, "> ") {
		case "", "1":
			thresholdsReady = true
		case "2":
			low, high, thresholdsReady = 20, 80, true
		case "3":
			for {
				low = promptIntRange(reader, output, "Seuil bas (%)", low, 0, 99)
				high = promptIntRange(reader, output, "Seuil haut (%)", high, 1, 100)
				if high > low {
					break
				}
				fmt.Fprintln(output, "Le seuil haut doit être strictement supérieur au seuil bas. Recommencez.")
			}
			thresholdsReady = true
		case "?":
			fmt.Fprintln(output, "Aide: le seuil bas réarme la surveillance; le seuil haut, strictement supérieur, autorise le démarrage lorsque le secteur est stable.")
		default:
			fmt.Fprintln(output, "Choix invalide. Recommencez.")
		}
	}
	policy.LowBattery, policy.HighBattery = intPointer(low), intPointer(high)
	fmt.Fprintln(output, "Stabilité secteur: [1] 30s  [2] 1m  [3] 5m  [4] personnalisée")
	stableFor, err := chooseDuration(reader, output, []time.Duration{30 * time.Second, time.Minute, 5 * time.Minute}, policy.StableFor.Value(30*time.Second), true)
	if err != nil {
		fmt.Fprintf(output, "Stabilité refusée: %v; politique précédente conservée.\n", err)
		return group.PowerRestore
	}
	policy.StableFor = stableFor
	fmt.Fprintln(output, "Fréquence NUT: [1] 5s  [2] 10s  [3] 30s  [4] personnalisée")
	pollInterval, err := chooseDuration(reader, output, []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second}, policy.PollInterval.Value(5*time.Second), false)
	if err != nil {
		fmt.Fprintf(output, "Fréquence refusée: %v; politique précédente conservée.\n", err)
		return group.PowerRestore
	}
	policy.PollInterval = pollInterval
	policy.Mode = promptEnum(reader, output, "Mode (sequential=un par un / parallel=indépendants en parallèle)", defaultString(policy.Mode, "sequential"), "sequential", "parallel")
	if policy.Mode == "sequential" {
		policy.Order = promptKnownIDs(reader, output, "Ordre de démarrage (virgules, facultatif)", policy.Order, cfg.Servers)
	}
	policy.FailurePolicy = promptEnum(reader, output, "Échec (stop=bloquer / continue=indépendants / retry=retenter / continue_retry=les deux)", defaultString(policy.FailurePolicy, "stop"), "stop", "continue", "retry", "continue_retry")
	if policy.FailurePolicy == "retry" || policy.FailurePolicy == "continue_retry" {
		policy.RetryLimit = promptIntRange(reader, output, "Nombre maximal de nouvelles tentatives", policy.RetryLimit, 0, 100)
		for {
			raw := promptDefault(reader, output, "Intervalle entre tentatives", policy.RetryInterval.Value(10*time.Second).String())
			value, err := time.ParseDuration(raw)
			if err == nil && value > 0 && value <= 24*time.Hour {
				policy.RetryInterval = Duration(value)
				break
			}
			fmt.Fprintln(output, "Intervalle invalide. Utilisez 30s, 2m ou 1m30s.")
		}
	}
	return &policy
}

func promptIntRange(reader *bufio.Reader, output io.Writer, label string, current, min, max int) int {
	for {
		value, err := strconv.Atoi(promptDefault(reader, output, label, strconv.Itoa(current)))
		if err == nil && value >= min && value <= max {
			return value
		}
		fmt.Fprintf(output, "Valeur invalide: saisissez un nombre entre %d et %d.\n", min, max)
	}
}

func promptEnum(reader *bufio.Reader, output io.Writer, label, current string, allowed ...string) string {
	for {
		value := strings.ToLower(promptDefault(reader, output, label, current))
		for _, candidate := range allowed {
			if value == candidate {
				return value
			}
		}
		fmt.Fprintln(output, "Valeur invalide. Choix autorisés:", strings.Join(allowed, ", "))
	}
}

func chooseDuration(reader *bufio.Reader, output io.Writer, presets []time.Duration, current time.Duration, allowZero bool) (Duration, error) {
	for {
		choice := prompt(reader, output, "> ")
		if choice == "?" {
			fmt.Fprintln(output, fieldHelp("durée"))
			continue
		}
		if index, err := strconv.Atoi(choice); err == nil && index >= 1 && index <= len(presets) {
			return Duration(presets[index-1]), nil
		}
		if choice == strconv.Itoa(len(presets)+1) {
			for {
				raw := promptDefault(reader, output, "Durée", current.String())
				value, err := time.ParseDuration(raw)
				if err == nil && value >= 0 && (allowZero || value > 0) && value <= 24*time.Hour {
					return Duration(value), nil
				}
				fmt.Fprintln(output, "Durée invalide. Utilisez 30s, 2m ou 1m30s.")
			}
		}
		if choice == "" {
			return Duration(current), nil
		}
		fmt.Fprintln(output, "Choix invalide. Recommencez.")
	}
}

func intPointer(value int) *int { return &value }

func configureGlobalVerification(reader *bufio.Reader, output io.Writer, cfg *Config) bool {
	value, err := promptVerification(reader, output, cfg.Global.Verification)
	if err != nil {
		fmt.Fprintf(output, "Délais de vérification refusés: %v\n", err)
		return false
	}
	logic := promptCheckLogic(reader, output, cfg.Global.CheckLogic)
	if sameVerification(value, cfg.Global.Verification) && logic == cfg.Global.CheckLogic {
		return false
	}
	cfg.Global.Verification = value
	cfg.Global.CheckLogic = logic
	fmt.Fprintln(output, "Délais globaux de vérification prêts.")
	return true
}

func promptVerification(reader *bufio.Reader, output io.Writer, current VerificationConfig) (VerificationConfig, error) {
	var err error
	if current.InitialDelay, err = promptDurationOverride(reader, output, "Attente avant la première vérification", current.InitialDelay, true); err != nil {
		return current, err
	}
	if current.Timeout, err = promptDurationOverride(reader, output, "Durée maximale des vérifications", current.Timeout, false); err != nil {
		return current, err
	}
	if current.Interval, err = promptDurationOverride(reader, output, "Intervalle entre deux vérifications", current.Interval, false); err != nil {
		return current, err
	}
	if current.Attempts, err = promptIntOverride(reader, output, "Nombre maximal de vérifications (0 = limité seulement par timeout)", current.Attempts); err != nil {
		return current, err
	}
	if current.PacketRetries, err = promptIntOverride(reader, output, "Nouveaux Magic Packets après échec", current.PacketRetries); err != nil {
		return current, err
	}
	return current, nil
}

func promptDurationOverride(reader *bufio.Reader, output io.Writer, label string, current *Duration, allowZero bool) (*Duration, error) {
	shown := "hérité"
	if current != nil {
		shown = time.Duration(*current).String()
	}
	for {
		value := prompt(reader, output, fmt.Sprintf("%s [%s] (- pour hériter): ", label, shown))
		if value == "" {
			return current, nil
		}
		if value == "-" {
			return nil, nil
		}
		duration, err := time.ParseDuration(value)
		if err == nil && duration >= 0 && (allowZero || duration > 0) && duration <= maxVerificationDuration {
			result := Duration(duration)
			return &result, nil
		}
		if _, numberErr := strconv.Atoi(value); numberErr == nil {
			fmt.Fprintf(output, "Format invalide. Essayez %ss. Formats acceptés: 30s, 2m, 1m30s.\n", value)
		} else {
			fmt.Fprintln(output, "Durée invalide. Formats acceptés: 30s, 2m, 1m30s. Recommencez.")
		}
	}
}

func sameVerification(a, b VerificationConfig) bool {
	return sameDuration(a.InitialDelay, b.InitialDelay) && sameDuration(a.Timeout, b.Timeout) && sameDuration(a.Interval, b.Interval) && sameInt(a.Attempts, b.Attempts) && sameInt(a.PacketRetries, b.PacketRetries)
}

func sameDuration(a, b *Duration) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func sameInt(a, b *int) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func promptIntOverride(reader *bufio.Reader, output io.Writer, label string, current *int) (*int, error) {
	shown := "hérité"
	if current != nil {
		shown = strconv.Itoa(*current)
	}
	for {
		value := prompt(reader, output, fmt.Sprintf("%s [%s] (- pour hériter): ", label, shown))
		if value == "" {
			return current, nil
		}
		if value == "-" {
			return nil, nil
		}
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed >= 0 && parsed <= 100 {
			return intPointer(parsed), nil
		}
		fmt.Fprintln(output, "Nombre invalide (0 à 100). Recommencez; les valeurs précédentes sont conservées.")
	}
}

func serverDeleteBlocker(cfg *Config, id string) string {
	for otherID, server := range cfg.Servers {
		for _, dependency := range server.Dependencies {
			if dependency == id {
				return "dépendance de la machine " + otherID
			}
		}
	}
	for groupID, group := range cfg.Groups {
		for _, serverID := range append(append([]string{}, group.Servers...), group.Dependencies...) {
			if serverID == id {
				return "référencée par le groupe " + groupID
			}
		}
	}
	return ""
}

func groupDeleteBlocker(cfg *Config, id string) string {
	for serverID, server := range cfg.Servers {
		for _, groupID := range server.Groups {
			if groupID == id {
				return "utilisé par la machine " + serverID
			}
		}
	}
	for _, automation := range cfg.Automations {
		if automation.Group == id {
			return "utilisé par l'automatisation " + automation.ID
		}
	}
	return ""
}

func valueOrDefault(value, fallback bool) bool {
	if value {
		return true
	}
	return fallback
}

func emptyAs(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func configureRemoteNUT(reader *bufio.Reader, output io.Writer, configured map[string]UPSConfig) error {
	var address string
	for {
		address = prompt(reader, output, "Adresse IP ou DNS du serveur NUT: ")
		if address == "annuler" || address == "retour" {
			return errors.New("configuration reportée")
		}
		if address != "" {
			break
		}
		fmt.Fprintln(output, "Adresse requise. Saisissez « annuler » pour reporter.")
	}
	var port string
	for {
		port = prompt(reader, output, "Port NUT [3493]: ")
		if port == "" {
			port = "3493"
		}
		value, err := strconv.Atoi(port)
		if err == nil && value >= 1 && value <= 65535 {
			break
		}
		fmt.Fprintln(output, "Port invalide (1 à 65535). Recommencez.")
	}
	if host, _, err := net.SplitHostPort(address); err == nil {
		address = net.JoinHostPort(host, port)
	} else {
		address = net.JoinHostPort(strings.Trim(address, "[]"), port)
	}
	server := UPSConfig{Address: address, Timeout: Duration(3 * time.Second)}
	fmt.Fprintln(output, "STARTTLS chiffre la connexion NUT et vérifie l'identité du serveur lorsqu'une CA est configurée.")
	if confirmPrompt(reader, output, "Utiliser STARTTLS ?") {
		server.TLS = true
		server.ServerName = prompt(reader, output, "Nom TLS du serveur [adresse]: ")
	} else {
		fmt.Fprintln(output, "ATTENTION: une connexion NUT distante sans TLS n'est pas chiffrée.")
		if !confirmPrompt(reader, output, "Autoriser explicitement cette connexion ?") {
			return errors.New("connexion distante non sécurisée refusée")
		}
		server.AllowInsecureRemote = true
	}

	for {
		fmt.Fprintf(output, "Connexion au serveur NUT %s...\n", server.Address)
		devices, err := listNUTUPS(context.Background(), server)
		if err == nil && len(devices) > 0 {
			serverID := prompt(reader, output, "Identifiant du serveur [nut]: ")
			if serverID == "" {
				serverID = "nut"
			}
			return selectNUTDevices(reader, output, serverID, server, devices, configured)
		}
		if err == nil {
			err = errors.New("aucun UPS disponible")
		}
		fmt.Fprintf(output, "Échec: %v\n", err)
		fmt.Fprintln(output, "[1] Réessayer\n[2] Modifier l'adresse ou le port\n[3] Configurer un autre serveur\n[4] Reporter la configuration")
		switch prompt(reader, output, "> ") {
		case "1":
			continue
		case "2", "3":
			return configureRemoteNUT(reader, output, configured)
		case "4":
			return errors.New("serveur NUT non enregistré")
		case "?":
			fmt.Fprintln(output, "Aide: réessayez après une erreur temporaire, modifiez l'adresse/port, choisissez un autre serveur ou reportez sans bloquer l'installation.")
		default:
			fmt.Fprintln(output, "Choix invalide. Recommencez.")
		}
	}
}

func selectNUTDevices(reader *bufio.Reader, output io.Writer, serverID string, server UPSConfig, devices []NUTDevice, configured map[string]UPSConfig) error {
	fmt.Fprintln(output, "UPS disponibles :")
	for i, device := range devices {
		fmt.Fprintf(output, "[%d] %s", i+1, device.Name)
		if device.Description != "" {
			fmt.Fprintf(output, " — %s", device.Description)
		}
		fmt.Fprintln(output)
	}
	var indices []int
	for {
		selection := prompt(reader, output, "Sélectionnez un ou plusieurs UPS (ex. 1,2 ou tous): ")
		var err error
		indices, err = parseSelection(selection, len(devices))
		if err == nil {
			break
		}
		fmt.Fprintln(output, err, "Recommencez.")
	}
	added := 0
	for _, index := range indices {
		device := devices[index]
		ups := server
		ups.Name = device.Name
		status := nutStatus(context.Background(), device.Name, ups)
		if status.Error != "" {
			fmt.Fprintf(output, "%s non enregistré: lecture de ups.status impossible: %s\n", device.Name, status.Error)
			continue
		}
		baseID := slug(serverID) + "-" + slug(device.Name)
		id := baseID
		if _, exists := configured[baseID]; exists {
			if confirmPrompt(reader, output, fmt.Sprintf("%s existe déjà. Le remplacer ?", baseID)) {
				id = baseID
			} else {
				id = uniqueUPSID(configured, baseID)
			}
		}
		configured[id] = ups
		added++
		fmt.Fprintf(output, "%s enregistré sous %q, état %s.\n", device.Name, id, status.RawStatus)
	}
	if added == 0 {
		return errors.New("aucun UPS validé")
	}
	return nil
}

func parseSelection(value string, count int) ([]int, error) {
	if strings.EqualFold(strings.TrimSpace(value), "tous") {
		result := make([]int, count)
		for i := range result {
			result[i] = i
		}
		return result, nil
	}
	seen := map[int]bool{}
	var result []int
	for _, part := range strings.Split(value, ",") {
		index, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || index < 1 || index > count {
			return nil, errors.New("sélection invalide")
		}
		if !seen[index-1] {
			seen[index-1] = true
			result = append(result, index-1)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("aucun UPS sélectionné")
	}
	return result, nil
}

func removeConfiguredUPS(reader *bufio.Reader, output io.Writer, cfg *Config) {
	configured := cfg.UPS
	ids := sortedUPSIDs(configured)
	if len(ids) == 0 {
		fmt.Fprintln(output, "Aucun UPS configuré.")
		return
	}
	for i, id := range ids {
		fmt.Fprintf(output, "[%d] %s (%s / %s)\n", i+1, id, configured[id].Address, configured[id].Name)
	}
	selection, err := parseSelection(prompt(reader, output, "UPS à supprimer: "), len(ids))
	if err != nil {
		fmt.Fprintln(output, err)
		return
	}
	for _, index := range selection {
		id := ids[index]
		blocked := ""
		for serverID, server := range cfg.Servers {
			if server.UPS != nil && server.UPS.UPS == id {
				blocked = "politique UPS de la machine " + serverID
			}
		}
		for groupID, group := range cfg.Groups {
			if group.UPS != nil && group.UPS.UPS == id {
				blocked = "politique UPS du groupe " + groupID
			}
		}
		for _, automation := range cfg.Automations {
			if automation.UPS == id {
				blocked = "automatisation " + automation.ID
			}
		}
		if blocked != "" {
			fmt.Fprintf(output, "%s non supprimé: utilisé par %s.\n", id, blocked)
			continue
		}
		delete(configured, id)
	}
}

func testConfiguredUPS(output io.Writer, configured map[string]UPSConfig) bool {
	ok := true
	for _, id := range sortedUPSIDs(configured) {
		status := nutStatus(context.Background(), id, configured[id])
		if status.Error != "" {
			ok = false
			fmt.Fprintf(output, "%s: ERREUR — %s\n", id, status.Error)
		} else {
			fmt.Fprintf(output, "%s: %s\n", id, status.RawStatus)
		}
	}
	return ok
}

func writeSetupConfig(path string, base []byte, ups map[string]UPSConfig, backup bool) error {
	cfg, err := loadConfigData(base)
	if err != nil {
		return err
	}
	cfg.UPS = cloneUPS(ups)
	return writeManagedConfig(path, base, cfg, backup)
}

func writeManagedConfig(path string, base []byte, cfg *Config, backup bool) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("nouvelle configuration refusée: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(base, &document); err != nil {
		return fmt.Errorf("YAML existant invalide: %w", err)
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return errors.New("racine YAML invalide")
	}
	root := document.Content[0]
	if err := replaceGlobalVerification(root, cfg.Global.Verification); err != nil {
		return err
	}
	if err := replaceGlobalCheckLogic(root, cfg.Global.CheckLogic); err != nil {
		return err
	}
	for _, section := range []struct {
		name  string
		value any
	}{
		{"networks", cfg.Networks},
		{"groups", cfg.Groups},
		{"servers", cfg.Servers},
		{"ups", cfg.UPS},
	} {
		if err := replaceYAMLSection(root, section.name, section.value); err != nil {
			return err
		}
	}
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return err
	}
	_ = encoder.Close()

	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	if err := rejectConfigSymlinks(path); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".wol.yaml.*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0640); err == nil {
		if _, err = temp.Write(encoded.Bytes()); err == nil {
			err = temp.Sync()
		}
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if _, err := LoadConfig(tempPath); err != nil {
		return fmt.Errorf("nouvelle configuration refusée: %w", err)
	}
	if err := secureConfigFile(tempPath, path); err != nil {
		return err
	}
	if backup {
		backupPath := path + ".bak-" + time.Now().UTC().Format("20060102T150405.000000000Z")
		file, openErr := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
		if openErr != nil {
			return fmt.Errorf("sauvegarde impossible: %w", openErr)
		}
		_, writeErr := file.Write(base)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil {
			return fmt.Errorf("sauvegarde impossible: %w", writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("sauvegarde impossible: %w", closeErr)
		}
		if err := secureConfigFile(backupPath, path); err != nil {
			return fmt.Errorf("sauvegarde impossible: %w", err)
		}
	}
	if runtime.GOOS == "windows" {
		// La cible prise en charge est Debian, où rename remplace atomiquement.
		// Ce chemin permet seulement les tests et l'usage développeur Windows.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}

func replaceYAMLSection(root *yaml.Node, name string, value any) error {
	node := &yaml.Node{}
	if err := node.Encode(value); err != nil {
		return err
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == name {
			root.Content[i+1] = node
			return nil
		}
	}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, node)
	return nil
}

func replaceGlobalVerification(root *yaml.Node, value VerificationConfig) error {
	var global *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "global" {
			global = root.Content[i+1]
			break
		}
	}
	if global == nil {
		if value == (VerificationConfig{}) {
			return nil
		}
		global = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "global"}, global)
	}
	if global.Kind != yaml.MappingNode {
		return errors.New("section global YAML invalide")
	}
	for i := 0; i+1 < len(global.Content); i += 2 {
		if global.Content[i].Value != "verification" {
			continue
		}
		if value == (VerificationConfig{}) {
			global.Content = append(global.Content[:i], global.Content[i+2:]...)
			return nil
		}
		node := &yaml.Node{}
		if err := node.Encode(value); err != nil {
			return err
		}
		global.Content[i+1] = node
		return nil
	}
	if value == (VerificationConfig{}) {
		return nil
	}
	node := &yaml.Node{}
	if err := node.Encode(value); err != nil {
		return err
	}
	global.Content = append(global.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "verification"}, node)
	return nil
}

func replaceGlobalCheckLogic(root *yaml.Node, value string) error {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "global" || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		global := root.Content[i+1]
		for j := 0; j+1 < len(global.Content); j += 2 {
			if global.Content[j].Value != "check_logic" {
				continue
			}
			if value == "" {
				global.Content = append(global.Content[:j], global.Content[j+2:]...)
			} else {
				global.Content[j+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
			}
			return nil
		}
		if value != "" {
			global.Content = append(global.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "check_logic"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
		}
		return nil
	}
	if value != "" {
		return errors.New("section global YAML absente")
	}
	return nil
}

func rejectConfigSymlinks(path string) error {
	if info, err := os.Lstat(filepath.Dir(path)); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("configuration refusée: le répertoire cible est un lien symbolique")
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("configuration refusée: la cible est un lien symbolique")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func prompt(reader *bufio.Reader, output io.Writer, text string) string {
	for {
		fmt.Fprint(output, text)
		line, _ := reader.ReadString('\n')
		value := strings.TrimSpace(line)
		if value != "?" || strings.TrimSpace(text) == ">" {
			return value
		}
		fmt.Fprintln(output, fieldHelp(text))
	}
}

func fieldHelp(label string) string {
	key := strings.ToLower(label)
	switch {
	case strings.Contains(key, "identifiant"):
		return "Aide: l'identifiant technique est court, unique et sans espace (lettres, chiffres, - ou _), par exemple truenas, pc-bureau ou maison. Il sera utilisé dans « wol wake IDENTIFIANT ». Le nom lisible peut contenir des espaces."
	case strings.Contains(key, "cidr"):
		return "Aide: le CIDR décrit le réseau IPv4, par exemple 192.168.1.0/24. Appuyez sur Entrée pour accepter la valeur détectée."
	case strings.Contains(key, "interface linux"):
		return "Aide: nom de l'interface réseau Linux qui enverra le paquet, par exemple enp3s0. Entrée conserve la valeur détectée."
	case strings.Contains(key, "adresse ipv4 source"):
		return "Aide: adresse locale utilisée comme source. Entrée conserve l'adresse détectée; utilisez - pour revenir au choix automatique."
	case strings.Contains(key, "broadcast local"):
		return "Aide: recommandé sur le même réseau; envoie au broadcast local et reste limité au segment directement connecté."
	case strings.Contains(key, "unicast"):
		return "Aide: envoie directement à l'IP de la machine; simple mais dépend de la conservation de l'entrée ARP."
	case strings.Contains(key, "broadcast dirigé"):
		return "Aide: traverse un routeur vers un autre sous-réseau; plus risqué et à activer seulement si le routeur est configuré pour cela."
	case strings.Contains(key, "relais"):
		return "Aide: un relais WoL authentifié envoie le paquet depuis le réseau distant; recommandé lorsque le broadcast routé est interdit."
	case strings.Contains(key, "icmp") || strings.Contains(key, "contrôle") || strings.Contains(key, "vérification"):
		return "Aide: ICMP est un ping simple mais parfois bloqué; TCP vérifie un port réellement ouvert (22, 80, 443); ARP indique une présence locale mais constitue une preuve plus faible. Avec any, un succès suffit; avec all, tous doivent réussir."
	case strings.Contains(key, "durée") || strings.Contains(key, "attente") || strings.Contains(key, "intervalle"):
		return "Aide: utilisez une durée Go comme 30s, 2m ou 1m30s. « hérité » signifie que la valeur vient du réseau ou des valeurs globales."
	case strings.Contains(key, "nut") || strings.Contains(key, "ups") || strings.Contains(key, "starttls"):
		return "Aide: NUT (Network UPS Tools) lit l'état d'un onduleur. OL=secteur présent, OB=sur batterie, LB=batterie faible, FSD=arrêt forcé. STARTTLS chiffre la connexion; WoL Manager ne commande jamais l'onduleur."
	case strings.Contains(key, "priorité"):
		return "Aide: les groupes sont traités par priorité croissante: 1 avant 10. Deux groupes ne peuvent pas partager la même priorité."
	default:
		return "Aide: saisissez une valeur, appuyez sur Entrée pour conserver le défaut, ou utilisez « annuler » au début d'un objet pour revenir sans l'enregistrer."
	}
}

func cloneUPS(source map[string]UPSConfig) map[string]UPSConfig {
	copy := make(map[string]UPSConfig, len(source))
	for id, ups := range source {
		copy[id] = ups
	}
	return copy
}

func sortedUPSIDs(configured map[string]UPSConfig) []string {
	ids := make([]string, 0, len(configured))
	for id := range configured {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func slug(value string) string {
	var result strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			result.WriteRune(r)
		} else if result.Len() > 0 && !strings.HasSuffix(result.String(), "-") {
			result.WriteByte('-')
		}
	}
	return strings.Trim(result.String(), "-")
}

func uniqueUPSID(configured map[string]UPSConfig, base string) string {
	if base == "" {
		base = "nut-ups"
	}
	if _, exists := configured[base]; !exists {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, exists := configured[candidate]; !exists {
			return candidate
		}
	}
}

const minimalConfig = `version: 1
global:
  port: 9
  transport_order: [local_broadcast, unicast, directed_broadcast, relay]
  checks:
    - {type: tcp, port: 22, timeout: 2s}
api:
  listen: 127.0.0.1:8080
  tokens_file: /etc/wol-manager/secrets/tokens.json
storage:
  state_file: /var/lib/wol-manager/state.json
  audit_file: /var/log/wol-manager/audit.jsonl
network_discovery:
  passive_enabled: true
  active_enabled: false
  allowed_subnets: []
  safeguards: {require_confirmation: true, max_packets: 20, rate_limit_pps: 2, timeout: 30s}
networks: {}
groups: {}
servers: {}
ups: {}
relays: {}
automations: []
`
