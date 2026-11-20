# Changelog

## 1.4.5 - en préparation

### Corrigé

- une configuration structurellement valide reste acceptée lorsque certains réseaux distants, VLAN, interfaces ou transports ne sont pas disponibles sur l'hôte actuel ; ces machines restent signalées `ready: false` et aucun réveil sans transport n'est autorisé ;
- après restauration d'une sauvegarde, WoL Manager avertit lorsque aucun réseau déclaré ne correspond à un sous-réseau IPv4 directement connecté, sans modifier ni supprimer les réseaux et machines restaurés.
- les ressources système de la version cible (Logrotate, modèles systemd et configuration exemple) sont maintenant couvertes par le manifeste signé, téléchargées avec leur SHA-256 et contrôlées après installation ;
- une mise à jour vers la même version répare les ressources ou permissions manquantes sans modifier une installation déjà conforme ;
- une ressource obligatoire absente déclenche désormais un échec et un rollback au lieu d'un résultat global de succès ;
- le rollback restaure également les copies auxiliaires conservées sous `/usr/local/lib/wol-manager` et `/usr/share/doc/wol-manager`.

### Migration depuis les anciennes versions

Les installateurs `1.2.1`, `1.2.3` et `1.3.0` peuvent bloquer une mise à jour lorsque le binaire candidat retourne `valid: true`, `ready: false`. Leur commande `wol update` atteint le binaire candidat, mais l'ancien installateur interprète encore ce diagnostic opérationnel comme une incompatibilité.

Le [bootstrap authentifié](scripts/migration/upgrade-network-validation.sh) vérifie le manifeste officiel signé, la signature Ed25519 et le SHA-256 de l'installateur avant de remplacer uniquement `/usr/local/lib/wol-manager/install.sh`. Il ne modifie ni le YAML, ni les secrets, ni les états, ni le binaire, ni les services. La mise à jour standard est ensuite relancée.

Pour la publication `1.4.5 dev`, utilisez le commit de publication complet et
vérifiez également le SHA-256 du script avant son exécution :

```sh
publication=$(curl -fsSL \
  https://api.github.com/repos/HaasGa-Factory/WOL-Manager/commits/dev \
  | sed -n 's/^[[:space:]]*"sha":[[:space:]]*"\([0-9a-f]\{40\}\)".*/\1/p' \
  | head -n1)

printf '%s\n' "$publication" | grep -Eq '^[0-9a-f]{40}$' || exit 1

curl -fSLo upgrade-network-validation.sh \
  "https://raw.githubusercontent.com/HaasGa-Factory/WOL-Manager/$publication/scripts/migration/upgrade-network-validation.sh"

echo '60de23bb3062ffbbbb508220ced9a0f2146a83b392d55c2fe87934d946f1ae4f  upgrade-network-validation.sh' \
  | sha256sum -c -

chmod 700 upgrade-network-validation.sh
sudo ./upgrade-network-validation.sh --channel dev --dry-run
sudo ./upgrade-network-validation.sh --channel dev
sudo wol update --channel dev
```

À partir de `1.4.1`, l'installateur reconnaît déjà `valid: true`, `ready: false`; ce bootstrap n'est donc pas nécessaire.

### Tests

- restauration depuis un autre sous-réseau, présence ou absence d'un réseau local, conservation des VLAN distants, bootstrap depuis `1.2.1`/`1.2.3`/`1.3.0`, signature et SHA-256, simulation, idempotence et préservation des données protégées.

## 1.4.4 - 2026-11-19

### Corrigé

- le fichier Logrotate est forcé en fins de ligne Unix `LF` dans Git et normalisé par l'installateur, même si une archive a été préparée depuis Windows ;
- une installation fraîche, une mise à jour et une relance déjà à jour réparent `/etc/logrotate.d/wol-manager` sans toucher au journal ni à ses archives ;
- la ressource système conservée sous `/usr/local/lib/wol-manager` est également normalisée afin que les mises à jour suivantes restent idempotentes ;
- le choix `Valider` du menu distingue désormais une configuration structurellement valide d'une configuration réellement opérationnelle et réutilise le même diagnostic de transports que `wol config check`.

### Tests

- la suite vérifie l'absence de caractères `CR`, la reconnaissance par `logrotate -d`, la migration d'un fichier CRLF, l'idempotence, les permissions et la conservation de l'historique d'audit.

## 1.4.3 - 2026-11-18

### Corrigé

- l'installation et les mises à jour créent ou normalisent `audit.jsonl` en `wol-manager:wol-manager` avec le mode `0640`, sans tronquer les événements existants ;
- le daemon journalise dans journald son démarrage, les transitions NUT significatives, l'armement des groupes, les reprises, les envois WoL, les vérifications et la fin des cycles ;
- les erreurs persistantes du journal d'audit ne sont plus répétées à chaque polling et un message signale son rétablissement.

### Sécurité

- les cibles symboliques ou non régulières restent refusées pendant la normalisation du journal ;
- les restrictions systemd, la journalisation JSONL indépendante et la politique logrotate existante sont conservées.

## 1.4.2 - 2026-11-17

### Modifié

- `wol config check` regroupe les machines ayant le même défaut de transport par réseau et affiche des étapes de correction lisibles ;
- l'installateur relaie ce rapport structuré sans concaténer toutes les machines sur une seule ligne ;
- le mode de compatibilité de l'ancienne unité combinée utilise un socket privé inscriptible au lieu d'échouer sur `/run/wol-manager` ;
- l'installateur migre de façon transactionnelle l'unité combinée v1.2.3 lorsqu'il est relancé, y compris si le binaire est déjà à jour, en conservant les états actif et activé ;
- le rollback restaure l'ancienne unité combinée et ses états sans laisser d'unité API partielle ;
- la sortie JSON et les codes de retour de la validation restent inchangés.

## 1.4.1 - 2026-11-17

### Ajouté

- commande permanente `wol daemon` dédiée aux jobs, à la surveillance NUT et à la reprise après coupure, sans serveur HTTP ;
- unités indépendantes `wol-manager.service` pour le daemon et `wol-manager-api.service` pour l'API REST ;
- quatre modes d'installation : CLI seule, daemon, API, ou daemon et API ;
- diagnostic de l'activité réelle du daemon et de chaque communication NUT dans `wol automation status` et `GET /api/v1/automations` ;
- reconfiguration des composants systemd sans réinstallation avec `install.sh --configure-services --service ...`.

### Modifié

- l'API délègue les jobs et automatisations au daemon local au lieu de posséder un second moteur ;
- une API démarrée sans daemon conserve les lectures sans état, mais refuse explicitement les opérations asynchrones avec `503 daemon_unavailable` ;
- l'installateur migre l'ancienne unité combinée vers les deux unités séparées en préservant configuration, jetons, jobs et cycles ;
- un mode de compatibilité temporaire maintient la surveillance NUT lorsqu'un ancien installateur a déjà remplacé le binaire sans encore migrer l'unité systemd.
- la chaîne de publication exige Go 1.26.11 afin d'intégrer le correctif de sécurité `net/http` contrôlé par `govulncheck`.
- une configuration syntaxiquement valide mais temporairement non opérationnelle ne bloque plus une mise à jour ; l'installateur conserve le YAML et détaille, par machine, les transports refusés ainsi que les commandes de correction et de vérification.

## 1.3.0 - en préparation

### Ajouté

- commande `wol backup` avec création, liste, inspection, vérification, restauration sélective et nettoyage ;
- sauvegarde de la configuration seule par défaut, avec inclusion explicite des jetons ou de l'état persistant ;
- restauration simulable avec `--dry-run`, sauvegarde de sécurité préalable et remplacement atomique ;
- restauration des jobs actifs dans l'état prudent `revalidation_required` et refus du state tant que le service est actif ;
- format `.wolbackup` borné et contrôlé par SHA-256, protégé en `0600`, sans binaires, caches ni journaux.

### Sécurité

- stockage système des sauvegardes sous `/var/backups/wol-manager`, hors du répertoire d'état détenu par le service ;
- refus des liens symboliques, chemins traversants, entrées dupliquées, archives trop volumineuses et cibles système hors périmètre ;
- conservation d'au moins une sauvegarde valide lors du nettoyage.

## 1.2.3 - 2026-11-05

- compatibilité de mise à jour avec l'agencement `1.2.1` qui ne contient pas encore la ressource Logrotate dans le répertoire de l'installateur ;
- conservation de la configuration Logrotate existante, ou avertissement non bloquant lorsqu'elle est absente, sans copier de ressource non signée ;
- documentation de l'amorçage unique de l'installateur système `1.2.1` avec le `dist/install.sh` vérifié du paquet `1.2.3` ;
- transition explicite d'un binaire `dev` vers le binaire stable `main`, même lorsque la version et la révision source sont identiques.

## 1.2.2 - 2026-11-03

- correction de la mise à jour depuis une configuration utilisant l'ancien transport `unicast` : le binaire signé ouvre désormais une migration guidée, confirmée et sauvegardée avant installation ;
- refus explicite et sans modification en mode non interactif lorsqu'une migration de configuration est nécessaire.

## 1.2.1 - 2026-11-03

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
- canal `dev` toujours temporaire, avec `main` conservé comme canal par défaut.

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
