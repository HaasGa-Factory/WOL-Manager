# Changelog

## 1.2.1 - en préparation

- aide contextuelle complète pour les commandes et sous-commandes, disponible sans charger la configuration ;
- aide `?` dans les menus guidés, avec explications NUT, groupes, séquences et reprise après coupure ;
- reprise automatique persistante après un cycle de coupure NUT réellement observé ;
- seuils batterie bas/haut, stabilité secteur et fréquence de surveillance configurables par groupe ;
- modes séquentiel et parallèle, ordre explicite, dépendances, politiques d'échec et tentatives bornées ;
- suspension et annulation immédiates des séquences automatiques sur `OB`, `LB`, `FSD`, état inconnu ou erreur NUT ;
- revalidation obligatoire après redémarrage, sans réveil automatique au seul constat d'un secteur présent ;
- contrôles multiples ICMP/TCP/ARP avec logique `any` (OU) ou `all` (ET) et ports TCP distincts ;
- séparation des tentatives de vérification et des nouveaux envois de Magic Packets ;
- état des automatismes disponible avec `wol automation status` et `GET /api/v1/automations` ;
- configuration guidée des politiques de reprise, sans édition YAML obligatoire.
- messages interactifs colorés avec désactivation automatique pour les journaux, JSON, redirections, `NO_COLOR` et terminaux incompatibles.
- distinction explicite entre héritage des contrôles et « aucune vérification », sans attente ni tentative inutile ;
- simulation de fin d'assistant avec sélection numérotée, aides de champ précises et progression visible avant le contrôle d'un réveil réel ;
- aide de création des jetons complétée par un exemple d'en-tête Bearer.

## 1.1.1 - 2026-10-16

- message explicite et sans modification lorsque `wol update --check` consulte un manifeste `main` signé de format 1 depuis une installation `dev` plus récente ;
- refus strict conservé pour les signatures invalides et les formats de manifeste inconnus ;
- canal `dev` toujours temporaire, avec `main` conservé comme canal par défaut ;
- remplacement explicite du binaire `dev` par le binaire `main` lorsque version et révision source sont identiques.

## 1.1.0-beta - 2026-10-16

- assistant unique pour réseaux, machines, groupes, dépendances et NUT ;
- gestion guidée accessible depuis `wol setup`, `wol menu` et `wol config` ;
- sauvegarde validée et atomique, protection contre les liens symboliques et permissions Debian `0640` ;
- simulations WoL guidées et confirmation obligatoire avant un réveil réel dans le menu ;
- `curl` vérifié dès l'installation pour garantir le fonctionnement ultérieur de `wol update`.
- autocomplétion Bash installée pour les commandes et sous-commandes (`wol m<Tab>`, `wol config m<Tab>`).
- détection des dépendances avec un `PATH` système déterministe, y compris pour `wol update --check` sans sudo ;
- aides CLI complètes et sans chargement de configuration pour `wol --help` et `wol config … --help` ;
- complétion Bash déplacée dans `/etc/bash_completion.d/wol-manager` sans toucher au fichier `wol` appartenant à Debian.
- installateur système désormais inclus dans le manifeste signé et mis à jour atomiquement avec le binaire.

## 1.0.0-beta.1 - 2026-10-14

- Première Beta : CLI, API `/api/v1`, moteur WoL multi-réseau et relais mTLS.
- Configuration YAML stricte, groupes, dépendances et politiques UPS NUT.
- Jobs persistants, annulation, reprise avec revalidation et automatisation de retour secteur.
- Découverte passive et diagnostic actif avec garde-fous et dry-run.
- Documentation, OpenAPI, installation systemd, tests et CI.
- Mini-release : installateur Debian 12/13 sans exigence Go, artefacts AMD64/ARM64, dépendances consenties et contrôle informatif des mises à jour APT.
- Mise à jour atomique avec conservation des données, rollback, journaux et commande `wol update` déléguée à un installateur root approuvé.
- Compilation locale AMD64/ARM64, manifeste signé Ed25519 et binaires versionnés dans `dist/`.
- Installation binaire uniquement : aucune installation de Go ni compilation de secours sur Debian.
- Mises à jour cohérentes par commit sur `main` et canal `dev` temporaire, avec rollback et conservation de systemd.
- Assistant NUT relançable : détection protocolaire locale/distante, plusieurs serveurs/UPS et diagnostic `wol ups test` en lecture seule.
