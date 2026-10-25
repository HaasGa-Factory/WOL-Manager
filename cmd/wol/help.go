package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func printCommandHelp(output io.Writer, args []string) error {
	key := args[0]
	if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		switch key {
		case "config", "token", "network", "ups", "automation", "api", "relay", "completion":
			key += " " + args[1]
		}
	}
	aliases := map[string]string{
		"config networks": "config network", "config server": "config machine", "config servers": "config machine",
		"config groups": "config group", "config ups": "config nut",
	}
	if canonical := aliases[key]; canonical != "" {
		key = canonical
	}
	text, ok := commandHelp[key]
	if !ok {
		return errUsage
	}
	renderCommandHelpTopic(output, key, text, helpRenderOptions{mode: outputColorMode(output), width: outputWidth(output)})
	return nil
}

type helpRow struct {
	command     string
	description string
}

type helpSection struct {
	title string
	text  []string
	rows  []helpRow
}

type helpRenderOptions struct {
	mode  colorMode
	width int
}

func renderRootHelp(output io.Writer, options helpRenderOptions) {
	options.width = clampWidth(options.width)
	renderHelpHeader(output, options)
	renderHelpSection(output, helpSection{title: "UTILISATION", text: []string{"wol [--config FILE] [--json] [--no-color] COMMANDE"}}, options)
	renderHelpSection(output, helpSection{title: "COMMANDES PRINCIPALES", rows: []helpRow{
		{command: "wol setup", description: "Assistant de configuration"},
		{command: "wol list", description: "Liste les machines"},
		{command: "wol status MACHINE", description: "Vérifie une machine"},
		{command: "wol wake MACHINE", description: "Réveille une machine et ses dépendances"},
		{command: "wol wake --group GROUPE", description: "Réveille un groupe"},
		{command: "wol wake --all", description: "Réveille toutes les machines actives"},
		{command: "wol menu", description: "Ouvre l'interface interactive"},
	}}, options)
	renderHelpSection(output, helpSection{title: "CONFIGURATION ET ACCÈS", rows: []helpRow{
		{command: "wol config check", description: "Valide la configuration"},
		{command: "wol config network|machine|group|nut|discovery", description: "Gestion guidée sans édition YAML"},
		{command: "wol config edit", description: "Ouvre l'édition YAML avancée"},
		{command: "wol token create|list|revoke", description: "Gère les jetons API"},
	}}, options)
	renderHelpSection(output, helpSection{title: "RÉSEAU, UPS ET SERVICES", rows: []helpRow{
		{command: "wol network test [MACHINE...]", description: "Explique les transports sans envoyer de paquet"},
		{command: "wol network discover [OPTIONS]", description: "Lance la découverte passive ou active"},
		{command: "wol ups status|test", description: "Consulte ou diagnostique les UPS NUT en lecture seule"},
		{command: "wol jobs", description: "Liste les tâches persistées"},
		{command: "wol automation status", description: "Affiche l'état des reprises NUT"},
		{command: "wol api serve", description: "Démarre l'API et les automatismes"},
		{command: "wol relay serve|check", description: "Exécute ou vérifie un relais distant"},
	}}, options)
	renderHelpSection(output, helpSection{title: "MAINTENANCE", rows: []helpRow{
		{command: "wol update [--check] [--channel main|dev]", description: "Vérifie ou installe une mise à jour"},
		{command: "wol completion bash", description: "Génère l'autocomplétion Bash"},
		{command: "wol version", description: "Affiche la version, le canal ou la révision source"},
		{command: "wol --help", description: "Affiche cette aide"},
	}}, options)
	renderHelpSection(output, helpSection{title: "OPTIONS GLOBALES", rows: []helpRow{
		{command: "--config FILE", description: "Utilise un autre fichier de configuration"},
		{command: "--json", description: "Produit une sortie JSON exploitable par un script"},
		{command: "--no-color", description: "Désactive les couleurs ANSI"},
		{command: "-h, --help", description: "Affiche l'aide générale ou détaillée"},
	}}, options)
	renderHelpTip(output, "Astuce : wol wake --help affiche les options détaillées du réveil.", options)
}

func renderHelpHeader(output io.Writer, options helpRenderOptions) {
	renderHelpHeaderTopic(output, "", options)
}

func renderHelpHeaderTopic(output io.Writer, topic string, options helpRenderOptions) {
	symbol := "⚡"
	locale := os.Getenv("LC_ALL")
	if locale == "" {
		locale = os.Getenv("LANG")
	}
	if locale == "C" || locale == "POSIX" {
		symbol = ">"
	}
	if topic == "" {
		fmt.Fprintf(output, "%s %s %s\n\n",
			styledTextMode(symbol, styleSection, options.mode),
			styledTextMode("WoL Manager", styleTitle, options.mode),
			styledTextMode("v"+version, styleMuted, options.mode),
		)
		fmt.Fprintln(output, styledTextMode("Gestion centralisée du Wake-on-LAN", styleDescription, options.mode))
		return
	}
	fmt.Fprintf(output, "%s %s %s\n",
		styledTextMode(symbol, styleSection, options.mode),
		styledTextMode("WoL Manager", styleTitle, options.mode),
		styledTextMode("— AIDE "+strings.ToUpper(topic), styleSection, options.mode),
	)
}

func renderCommandHelp(output io.Writer, raw string, options helpRenderOptions) {
	renderCommandHelpTopic(output, "", raw, options)
}

func renderCommandHelpTopic(output io.Writer, topic, raw string, options helpRenderOptions) {
	options.width = clampWidth(options.width)
	renderHelpHeaderTopic(output, topic, options)
	for _, section := range parseCommandHelp(raw) {
		renderHelpSection(output, section, options)
	}
	renderHelpTip(output, "Astuce : wol --help affiche toutes les commandes disponibles.", options)
}

func renderHelpTip(output io.Writer, tip string, options helpRenderOptions) {
	fmt.Fprintln(output)
	for _, line := range wrapText(tip, options.width) {
		fmt.Fprintln(output, styledTextMode(line, styleMuted, options.mode))
	}
}

func parseCommandHelp(raw string) []helpSection {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	sections := make([]helpSection, 0, 4)
	for index := 0; index < len(lines); {
		if strings.TrimSpace(lines[index]) == "" {
			index++
			continue
		}
		line := strings.TrimSpace(lines[index])
		if strings.HasPrefix(line, "Usage:") {
			section := helpSection{title: "UTILISATION", text: []string{strings.TrimSpace(strings.TrimPrefix(line, "Usage:"))}}
			index++
			for index < len(lines) && strings.TrimSpace(lines[index]) != "" {
				section.text = append(section.text, strings.TrimSpace(lines[index]))
				index++
			}
			sections = append(sections, section)
			continue
		}
		if title, rest, ok := helpLabel(line); ok {
			section := helpSection{title: title}
			if title == "OPTIONS" || title == "SOUS-COMMANDES" {
				index++
				for index < len(lines) && strings.TrimSpace(lines[index]) != "" {
					section.rows = append(section.rows, splitHelpRow(lines[index]))
					index++
				}
			} else {
				paragraph := []string{rest}
				index++
				for index < len(lines) && strings.TrimSpace(lines[index]) != "" {
					if _, _, isLabel := helpLabel(strings.TrimSpace(lines[index])); isLabel {
						break
					}
					paragraph = append(paragraph, strings.TrimSpace(lines[index]))
					index++
				}
				if text := strings.TrimSpace(strings.Join(paragraph, " ")); text != "" {
					section.text = append(section.text, text)
				}
			}
			sections = append(sections, section)
			continue
		}
		section := helpSection{title: "DESCRIPTION"}
		paragraph := make([]string, 0, 4)
		for index < len(lines) && strings.TrimSpace(lines[index]) != "" {
			line = strings.TrimSpace(lines[index])
			if title, rest, ok := helpLabel(line); ok {
				if len(paragraph) > 0 {
					section.text = append(section.text, strings.Join(paragraph, " "))
					paragraph = paragraph[:0]
				}
				if len(section.text) > 0 {
					sections = append(sections, section)
				}
				sections = append(sections, helpSection{title: title, text: []string{rest}})
				section = helpSection{title: "DESCRIPTION"}
			} else {
				paragraph = append(paragraph, line)
			}
			index++
		}
		if len(paragraph) > 0 {
			section.text = append(section.text, strings.Join(paragraph, " "))
		}
		if len(section.text) > 0 {
			sections = append(sections, section)
		}
	}
	return sections
}

func helpLabel(line string) (string, string, bool) {
	labels := []string{"Options:", "Sous-commandes:", "Actions:", "Paramètres:", "Valeurs par défaut:", "Contraintes:", "Exemple:", "Exemples:"}
	for _, label := range labels {
		if strings.HasPrefix(line, label) {
			title := strings.ToUpper(strings.TrimSuffix(label, ":"))
			if title == "EXEMPLE" {
				title = "EXEMPLES"
			}
			return title, strings.TrimSpace(strings.TrimPrefix(line, label)), true
		}
	}
	return "", "", false
}

func splitHelpRow(line string) helpRow {
	line = strings.TrimSpace(line)
	for index := 0; index < len(line)-1; index++ {
		if line[index] == ' ' && line[index+1] == ' ' {
			return helpRow{command: strings.TrimSpace(line[:index]), description: strings.TrimSpace(line[index:])}
		}
	}
	return helpRow{command: line}
}

func renderHelpSection(output io.Writer, section helpSection, options helpRenderOptions) {
	fmt.Fprintln(output)
	fmt.Fprintln(output, styledTextMode(section.title, styleSection, options.mode))
	if len(section.rows) > 0 {
		renderHelpRows(output, section.rows, options)
		return
	}
	for _, paragraph := range section.text {
		for _, line := range wrapText(paragraph, options.width) {
			fmt.Fprintln(output, styledTextMode(line, styleDescription, options.mode))
		}
	}
}

func renderHelpRows(output io.Writer, rows []helpRow, options helpRenderOptions) {
	leftWidth := 0
	for _, row := range rows {
		if width := visibleWidth(row.command); width > leftWidth {
			leftWidth = width
		}
	}
	gap := 4
	stacked := options.width-leftWidth-gap < 24
	for _, row := range rows {
		command := styledTextMode(row.command, styleCommand, options.mode)
		if row.description == "" {
			fmt.Fprintln(output, command)
			continue
		}
		if stacked {
			fmt.Fprintln(output, command)
			for _, line := range wrapText(row.description, options.width-4) {
				fmt.Fprintf(output, "    %s\n", styledTextMode(line, styleDescription, options.mode))
			}
			continue
		}
		descriptionWidth := options.width - leftWidth - gap
		wrapped := wrapText(row.description, descriptionWidth)
		padding := strings.Repeat(" ", leftWidth-visibleWidth(row.command)+gap)
		fmt.Fprintf(output, "%s%s%s\n", command, padding, styledTextMode(wrapped[0], styleDescription, options.mode))
		continuation := strings.Repeat(" ", leftWidth+gap)
		for _, line := range wrapped[1:] {
			fmt.Fprintf(output, "%s%s\n", continuation, styledTextMode(line, styleDescription, options.mode))
		}
	}
}

// printInteractiveMenu keeps interactive choices visually consistent with the
// command help while leaving input handling and menu actions unchanged.
func printInteractiveMenu(output io.Writer, title string, rows []helpRow) {
	options := helpRenderOptions{mode: outputColorMode(output), width: outputWidth(output)}
	renderInteractiveMenu(output, title, rows, options)
}

func renderInteractiveMenu(output io.Writer, title string, rows []helpRow, options helpRenderOptions) {
	fmt.Fprintln(output)
	renderInteractiveMenuHeader(output, title, options)
	renderHelpSection(output, helpSection{title: "ACTIONS", rows: rows}, options)
	fmt.Fprintln(output)
	for _, line := range wrapText("Saisissez un numéro, ou ? pour afficher l’aide. Les flèches ne sont pas utilisées.", options.width) {
		fmt.Fprintln(output, styledTextMode(line, styleMuted, options.mode))
	}
}

func renderInteractiveMenuHeader(output io.Writer, title string, options helpRenderOptions) {
	symbol := "⚡"
	locale := os.Getenv("LC_ALL")
	if locale == "" {
		locale = os.Getenv("LANG")
	}
	if locale == "C" || locale == "POSIX" {
		symbol = ">"
	}
	fmt.Fprintf(output, "%s %s %s\n",
		styledTextMode(symbol, styleSection, options.mode),
		styledTextMode("WoL Manager", styleTitle, options.mode),
		styledTextMode("— "+strings.ToUpper(title), styleSection, options.mode),
	)
}

func printInteractiveViewHeader(output io.Writer, title string) {
	options := helpRenderOptions{mode: outputColorMode(output), width: outputWidth(output)}
	fmt.Fprintln(output)
	renderInteractiveMenuHeader(output, title, options)
	fmt.Fprintln(output)
}

func printInformationBlock(output io.Writer, title string, rows []helpRow) {
	mode := outputColorMode(output)
	width := 0
	for _, row := range rows {
		if current := visibleWidth(row.command); current > width {
			width = current
		}
	}
	fmt.Fprintf(output, "- %s\n", styledTextMode(title, styleCommand, mode))
	for _, row := range rows {
		padding := strings.Repeat(" ", width-visibleWidth(row.command))
		fmt.Fprintf(output, "  %s%s : %s\n", styledTextMode(row.command, styleMuted, mode), padding, styledTextMode(row.description, styleDescription, mode))
	}
}

func printValueList(output io.Writer, title string, values []string) {
	rows := make([]helpRow, 0, len(values))
	for _, value := range values {
		rows = append(rows, helpRow{command: "- " + value})
	}
	printChoiceList(output, title, rows)
}

func printChoiceList(output io.Writer, title string, rows []helpRow) {
	renderHelpSection(output, helpSection{title: title, rows: rows}, helpRenderOptions{
		mode:  outputColorMode(output),
		width: outputWidth(output),
	})
}

var commandHelp = map[string]string{
	"setup": `Usage: sudo wol [--config FILE] setup

Assistant complet pour les réseaux, machines, groupes, délais de vérification et UPS NUT.
Une configuration existante est conservée en mémoire jusqu'à la confirmation finale. L'ancienne
version est sauvegardée et la nouvelle est validée avant une écriture atomique. Aucune simulation
ne transmet de paquet WoL réel. Dans les menus, saisissez ? pour afficher l'aide contextuelle.`,
	"update": `Usage: wol update --check [--channel main|dev]
       sudo wol update [--channel main|dev]

Options:
  --check          vérifie sans installer et sans modifier de fichier
  --channel CANAL  main par défaut; dev uniquement pour cette commande

Le manifeste signé et le SHA-256 sont vérifiés avant toute installation. Une mise à jour demande
confirmation, sauvegarde le binaire, le remplace atomiquement et effectue un rollback en cas d'échec.
Le canal dev n'est jamais mémorisé comme canal par défaut.`,
	"completion": `Usage: wol completion bash

Génère le script d'autocomplétion Bash sur la sortie standard. L'installateur le déploie
automatiquement; cette commande permet de l'inspecter ou de l'installer manuellement.`,
	"completion bash": `Usage: wol completion bash

Génère le script d'autocomplétion Bash sur la sortie standard.`,
	"list": `Usage: wol [--json] list

Liste les machines configurées. La sortie humaine affiche l'identifiant, l'adresse IPv4 et le nom.
Après l'installation, l'utilisateur ayant lancé sudo est ajouté au groupe wol-manager; reconnectez-vous
ou exécutez "newgrp wol-manager" avant d'utiliser cette commande sans sudo.`,
	"status": `Usage: wol [--json] status MACHINE

Exécute les contrôles ICMP, TCP ou ARP configurés sans envoyer de Magic Packet.
Une machine inconnue retourne le code 1; une machine connue mais injoignable reste un résultat valide.`,
	"wake": `Usage: wol [--json] wake MACHINE [--dry-run] [--verification-delay DURÉE]
       wol [--json] wake --group GROUPE [--dry-run]
       wol [--json] wake --all [--dry-run]

Options:
  --dry-run                    calcule le plan sans envoyer de paquet ni attendre
  --verification-delay DURÉE   surcharge ponctuelle, de 0s à 24h
  --group GROUPE               réveille un groupe
  --all                         réveille toutes les machines actives

MACHINE, --group et --all sont strictement mutuellement exclusifs. Une combinaison contradictoire
retourne le code 2 sans calculer ni exécuter de plan. Les dépendances, politiques UPS, transports
et vérifications configurés restent obligatoires.`,
	"config": `Usage: wol [--config FILE] config SOUS-COMMANDE

Sous-commandes:
  check       valide strictement la configuration
  network     gestion guidée des réseaux
  machine     gestion guidée des machines
  group       gestion guidée des groupes et reprises NUT
  nut         gestion guidée des serveurs NUT et UPS
  discovery   activation guidée de la découverte réseau
  edit        édition YAML avancée avec $EDITOR

Les alias networks, server, servers, groups et ups restent acceptés.`,
	"config check": `Usage: wol [--config FILE] [--json] config check

Charge et valide strictement le YAML, les références, dépendances, transports, contrôles,
politiques UPS et reprises automatiques. Aucun fichier n'est modifié. Cette lecture fonctionne sans
sudo après activation du groupe wol-manager; les modifications de /etc continuent à exiger sudo.`,
	"config edit": `Usage: sudo wol [--config FILE] config edit

Ouvre le YAML avec $EDITOR (vi par défaut). Réservé aux paramètres avancés non exposés par
l'assistant. Utilisez ensuite "wol config check" avant de redémarrer le service.`,
	"config network": `Usage: sudo wol [--config FILE] config network

Actions: ajouter, modifier, supprimer ou revenir.
Paramètres: identifiant, CIDR IPv4, interface, IP source, broadcast local, unicast et broadcast dirigé.
Valeurs par défaut: interface et IP source automatiques; broadcast local proposé à la création.
Contraintes: identifiant et CIDR valides; suppression refusée si une machine utilise le réseau.
Les relais, contrôles et temporisations réseau avancés restent disponibles dans le YAML.`,
	"config machine": `Usage: sudo wol [--config FILE] config machine

Actions: ajouter, modifier, supprimer ou revenir.
Paramètres: identifiant, nom, MAC, IPv4, réseau, groupes, dépendances, contrôles, logique any/all
délais de vérification et politique de sécurité UPS facultative.
Contraintes: MAC, IPv4 et réseau valides; suppression refusée tant que la machine est référencée.
Une politique UPS définie pour la machine remplace celle héritée de ses groupes.`,
	"config group": `Usage: sudo wol [--config FILE] config group

Actions: ajouter, modifier, supprimer ou revenir.
Paramètres: identifiant, priorité, machines, dépendances, sécurité UPS et reprise NUT facultatives.
La sécurité UPS conditionne chaque réveil et peut être surchargée par une machine.
La reprise configure UPS, seuils bas/haut, stabilité, fréquence, mode sequential/parallel,
ordre, politique d'échec et tentatives.
Contraintes: références existantes, priorité unique et seuil haut strictement supérieur au seuil bas.`,
	"config nut": `Usage: sudo wol [--config FILE] config nut

Actions: détecter localement, ajouter ou modifier un serveur, sélectionner ou supprimer des UPS et tester.
Paramètres: adresse IP/DNS, port, STARTTLS éventuel et UPS à surveiller.
Contraintes: opération strictement en lecture; aucun scan réseau, paquet d'arrêt, FSD, installation,
modification ou redémarrage de NUT. Les CA privées et fichiers d'identifiants se configurent dans le YAML avancé.`,
	"config discovery": `Usage: sudo wol [--config FILE] config discovery

Configure la découverte passive et active, les sous-réseaux CIDR autorisés, la limite de paquets,
le débit, le timeout et l'autorisation API. Le mode actif exige à la fois cette activation globale
et l'autorisation ponctuelle --allow-active-scan; cette option seule n'active jamais la fonction.`,
	"token": `Usage: sudo wol token create [--id IDENTIFIANT] [--permissions LISTE]
       sudo wol token list
       sudo wol token revoke --id IDENTIFIANT

Permissions séparées par des virgules: read (consultation), wake (réveil) et admin (administration).
Accordez le minimum nécessaire. Le secret créé est affiché une seule fois; seul son hash est enregistré.
"list" n'affiche ni secret ni hash; "revoke" désactive le jeton après confirmation.`,
	"token create": `Usage: sudo wol token create [--id IDENTIFIANT] [--permissions read,wake,admin]

Valeurs par défaut: id admin; permissions read,wake,admin. Les permissions acceptées sont read,
wake et admin. Protégez immédiatement le jeton affiché: il ne peut pas être relu ensuite.
Un processus API actif prend en compte le nouveau jeton dès la requête suivante, sans redémarrage.

Exemple:
  TOKEN='<jeton-affiché-une-fois>'
  curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/api/v1/health`,
	"token list": `Usage: sudo wol token list

Liste les identifiants, permissions et états des jetons. Les secrets et hashes ne sont jamais affichés.`,
	"token revoke": `Usage: sudo wol token revoke --id IDENTIFIANT

Désactive atomiquement un jeton après confirmation. Cette action ne révèle ni secret ni hash et
le processus API le refuse dès la requête suivante, sans redémarrage.`,
	"network": `Usage: wol network test [MACHINE...]
       wol [--json] network discover [--active] [--allow-active-scan] [--dry-run]

"test" explique le plan de transport sans envoyer de paquet. "discover" observe passivement le
réseau; le mode actif exige les garde-fous configurés et une confirmation explicite.`,
	"network test": `Usage: wol network test [MACHINE...]

Affiche en JSON les transports retenus ou refusés. Sans identifiant, toutes les machines sont
analysées. Aucun paquet WoL ni trafic de découverte n'est envoyé. Le code vaut 1 si au moins une
machine demandée est inconnue ou sans plan, même si les autres résultats sont valides.`,
	"network discover": `Usage: wol [--json] network discover [--active] [--allow-active-scan] [--dry-run]

Options:
  --active              active le diagnostic réseau configuré
  --allow-active-scan   confirmation explicite pour un appel non interactif
  --dry-run             calcule le diagnostic actif sans envoyer de Magic Packet

Le mode actif reste limité aux sous-réseaux, budget, débit et délai configurés.`,
	"ups": `Usage: wol [--json] ups status|test

Consulte les UPS configurés en lecture seule. Aucune commande électrique ou NUT de contrôle
n'est envoyée.`,
	"ups status": `Usage: wol [--json] ups status

Affiche ups.status et les métriques disponibles. Une mesure absente reste inconnue et n'est jamais
interprétée comme zéro.`,
	"ups test": `Usage: wol [--json] ups test

Teste tous les UPS configurés et retourne le code 1 si aucun UPS n'existe ou si un diagnostic échoue.`,
	"jobs": `Usage: wol jobs

Affiche en JSON les tâches persistées et leurs états: queued, running, revalidation_required,
completed, failed ou cancelled. Cette commande ne modifie ni ne reprend une tâche.`,
	"automation": `Usage: wol automation status

Consulte en lecture seule les cycles NUT, politiques de reprise et tâches persistées.`,
	"automation status": `Usage: wol automation status

Affiche en JSON les cycles de coupure observés, groupes terminés, progression, raisons d'attente,
politiques et jobs. La surveillance et la reprise automatiques fonctionnent dans le processus
permanent "wol api serve", normalement lancé par systemd.`,
	"menu": `Usage: sudo wol [--config FILE] menu

Menu interactif de consultation, validation, configuration, simulation et réveil confirmé.
La sortie humaine utilise des libellés français et des durées lisibles; --json conserve les formats
structurés. La gestion détaillée ouvre le même assistant que "wol setup". Saisissez ? pour l'aide contextuelle.`,
	"api": `Usage: wol api serve

Démarre l'API authentifiée et le moteur permanent de surveillance/reprise NUT.
URL locale par défaut: http://127.0.0.1:8080/api/v1. Santé: /api/v1/health.
Exemple: TOKEN='secret-affiché-une-fois'; curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/api/v1/health`,
	"api serve": `Usage: wol api serve

Démarre l'API REST, le stockage des jobs et le moniteur de reprise électrique. Une écoute non
loopback exige TLS. Toutes les routes, y compris /api/v1/health, exigent « Authorization: Bearer <jeton> ».
La spécification est api/openapi.yaml. Arrêtez au premier plan avec Ctrl+C ou le service avec systemctl.`,
	"relay": `Usage: wol relay serve --id ID --cert FILE --key FILE --client-ca FILE [--listen ADRESSE]
       wol relay check ID

Un relais envoie le Magic Packet depuis un autre réseau lorsque le broadcast local ne peut pas l'atteindre.
Il doit avoir un identifiant déclaré dans relays, une URL HTTPS, une CA et des certificats client/serveur mTLS.
Les certificats doivent venir de votre PKI ou être créés explicitement; WoL Manager ne génère jamais une PKI de production automatiquement.`,
	"relay serve": `Usage: wol relay serve --id ID --cert FILE --key FILE --client-ca FILE [--listen 127.0.0.1:9443]

Options obligatoires: identifiant de relais configuré, certificat serveur, clé privée et CA client.
Le relais n'accepte que des identifiants de machines présents dans sa configuration. Déclarez le même
identifiant sous relays dans la configuration principale puis associez-le au réseau. « relais inconnu »
signifie que cet identifiant n'existe pas dans cette section.`,
	"relay check": `Usage: wol relay check ID

Teste la santé HTTPS/mTLS d'un relais configuré sans envoyer de Magic Packet.`,
	"version": `Usage: wol version [--channel|--source-revision|--commit]

Sans option, affiche la version. --channel affiche le canal compilé. --source-revision et --commit
affichent la révision Git des sources utilisées pour construire le binaire.`,
}

func printContextHelp(output io.Writer, topic string) {
	renderContextHelp(output, topic, helpRenderOptions{mode: outputColorMode(output), width: outputWidth(output)})
}

func renderContextHelp(output io.Writer, topic string, options helpRenderOptions) {
	page, ok := contextHelpPages[topic]
	if !ok {
		return
	}
	options.width = clampWidth(options.width)
	renderHelpHeaderTopic(output, page.title, options)
	for _, section := range page.sections {
		renderHelpSection(output, section, options)
	}
	renderHelpTip(output, page.tip, options)
}

func printFieldHelp(output io.Writer, label string) {
	renderFieldHelp(output, label, helpRenderOptions{mode: outputColorMode(output), width: outputWidth(output)})
}

func renderFieldHelp(output io.Writer, label string, options helpRenderOptions) {
	options.width = clampWidth(options.width)
	renderHelpHeaderTopic(output, "CONTEXTUELLE", options)
	renderHelpSection(output, helpSection{title: "DESCRIPTION", text: []string{fieldHelp(label)}}, options)
	renderHelpTip(output, "Astuce : Entrée conserve le défaut affiché; l'aide ne modifie jamais la valeur.", options)
}

type contextHelpPage struct {
	title    string
	sections []helpSection
	tip      string
}

var contextHelpPages = map[string]contextHelpPage{
	"setup": {
		title: "CONFIGURATION",
		sections: []helpSection{
			{title: "DESCRIPTION", text: []string{"Prépare les réseaux, machines, groupes, délais, découverte et UPS sans modifier directement le YAML. Rien n'est écrit avant le résumé, la validation et la confirmation finale."}},
			{title: "NAVIGATION", rows: []helpRow{{command: "1", description: "Réseaux WoL"}, {command: "2", description: "Machines"}, {command: "3", description: "Groupes et dépendances"}, {command: "4", description: "Serveurs NUT et UPS"}, {command: "5", description: "Résumé, validation et enregistrement"}, {command: "6", description: "Annuler sans modifier"}, {command: "7", description: "Délais globaux de vérification"}, {command: "8", description: "Découverte réseau"}}},
			{title: "SÉCURITÉ", text: []string{"La simulation finale n'envoie aucun Magic Packet. « annuler » ou « retour » abandonne l'objet courant sans l'enregistrer."}},
		},
		tip: "Astuce : saisissez ? dans un champ pour obtenir son aide sans changer sa valeur.",
	},
	"network": {
		title: "RÉSEAUX",
		sections: []helpSection{
			{title: "DESCRIPTION", text: []string{"Un réseau relie une machine à une interface Linux et aux transports WoL autorisés. Sa suppression est refusée tant qu'une machine l'utilise."}},
			{title: "ACTIONS", rows: []helpRow{{command: "1", description: "Ajouter un réseau"}, {command: "2", description: "Modifier un réseau"}, {command: "3", description: "Supprimer un réseau inutilisé"}, {command: "4", description: "Revenir au menu précédent"}}},
			{title: "COMMANDE ÉQUIVALENTE", text: []string{"sudo wol config network"}},
		},
		tip: "Astuce : le broadcast local est généralement le choix recommandé sur le même sous-réseau.",
	},
	"machine": {
		title: "MACHINES",
		sections: []helpSection{
			{title: "DESCRIPTION", text: []string{"Une machine possède une adresse MAC, une IPv4, un réseau, des dépendances et des contrôles de disponibilité. La simulation n'envoie jamais de paquet."}},
			{title: "ACTIONS", rows: []helpRow{{command: "1", description: "Ajouter une machine"}, {command: "2", description: "Modifier une machine"}, {command: "3", description: "Supprimer une machine non référencée"}, {command: "4", description: "Revenir au menu précédent"}}},
			{title: "COMMANDE ÉQUIVALENTE", text: []string{"sudo wol config machine"}},
		},
		tip: "Astuce : utilisez wol wake IDENTIFIANT --dry-run pour vérifier le plan sans envoyer de paquet.",
	},
	"group": {
		title: "GROUPES",
		sections: []helpSection{
			{title: "DESCRIPTION", text: []string{"Un groupe ordonne plusieurs machines. Ses dépendances restent obligatoires en mode séquentiel comme en mode parallèle."}},
			{title: "ACTIONS", rows: []helpRow{{command: "1", description: "Ajouter un groupe"}, {command: "2", description: "Modifier un groupe"}, {command: "3", description: "Supprimer un groupe non référencé"}, {command: "4", description: "Revenir au menu précédent"}}},
			{title: "COMMANDE ÉQUIVALENTE", text: []string{"sudo wol config group"}},
		},
		tip: "Astuce : utilisez wol wake --group GROUPE pour réveiller un groupe configuré.",
	},
	"nut": {
		title: "NUT ET UPS",
		sections: []helpSection{
			{title: "DESCRIPTION", text: []string{"NUT reste facultatif et strictement lu. Network UPS Tools permet de lire l'état d'un onduleur; WoL Manager ne scanne pas le réseau, ne modifie pas NUT et n'envoie aucune commande électrique."}},
			{title: "ÉTATS", rows: []helpRow{{command: "OL", description: "Secteur présent"}, {command: "OB", description: "Fonctionnement sur batterie"}, {command: "LB", description: "Batterie faible"}, {command: "FSD", description: "Arrêt forcé signalé par NUT"}}},
			{title: "COMMANDE ÉQUIVALENTE", text: []string{"sudo wol config nut"}},
		},
		tip: "Astuce : wol ups test vérifie les UPS configurés en lecture seule.",
	},
	"menu": {
		title: "MENU PRINCIPAL",
		sections: []helpSection{
			{title: "NAVIGATION", rows: []helpRow{{command: "1 à 4", description: "Consulter machines, réseaux, groupes et UPS"}, {command: "5", description: "Valider la configuration"}, {command: "6", description: "Ouvrir l'assistant de configuration"}, {command: "7", description: "Simuler un réveil sans paquet"}, {command: "8", description: "Réveil réel après confirmation explicite"}, {command: "9", description: "Consulter les automatismes"}, {command: "0", description: "Quitter"}}},
			{title: "SÉCURITÉ", text: []string{"Une réponse vide ou ambiguë n'envoie aucun paquet. Le réveil réel exige toujours une sélection valide puis une confirmation explicite."}},
		},
		tip: "Astuce : chaque action reste aussi disponible par une commande documentée dans wol --help.",
	},
}
