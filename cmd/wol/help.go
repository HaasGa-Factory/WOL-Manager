package main

import (
	"fmt"
	"io"
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
	fmt.Fprintf(output, "WoL Manager %s\n\n%s\n", version, text)
	return nil
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
et délais de vérification.
Contraintes: MAC, IPv4 et réseau valides; suppression refusée tant que la machine est référencée.
Les transports et politiques UPS historiques avancés restent disponibles dans le YAML.`,
	"config group": `Usage: sudo wol [--config FILE] config group

Actions: ajouter, modifier, supprimer ou revenir.
Paramètres: identifiant, priorité, machines, dépendances et reprise NUT facultative.
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
Un processus API actif prend en compte le nouveau jeton dès la requête suivante, sans redémarrage.`,
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
	messages := map[string]string{
		"setup":   "Les sections peuvent être préparées dans n'importe quel ordre. Saisissez ? dans un champ pour son aide sans changer la valeur; « annuler » ou « retour » à la création d'un objet revient au menu. Rien n'est écrit avant Résumé, validation et enregistrement.",
		"network": "Un réseau relie une machine à une interface et aux transports WoL autorisés. Sa suppression est refusée tant qu'une machine l'utilise.",
		"machine": "Une machine possède une MAC, une IPv4, un réseau, des dépendances et des contrôles. La simulation n'envoie jamais de paquet.",
		"group":   "Un groupe ordonne plusieurs machines. Ses dépendances restent obligatoires en mode séquentiel comme en mode parallèle.",
		"nut":     "NUT reste facultatif et strictement lu. WoL Manager ne scanne pas le réseau, ne modifie pas NUT et n'envoie aucune commande électrique.",
		"menu":    "Consultez les objets avec 1 à 4, validez avec 5, configurez avec 6, simulez avec 7 et confirmez explicitement tout réveil réel avec 8.",
	}
	if message := messages[topic]; message != "" {
		fmt.Fprintln(output, "Aide:", message)
	}
}
