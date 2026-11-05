# Rapport de validation de la documentation WoL Manager v1.2.3

## 1. Référence analysée

- Projet : `HaasGa-Factory/WOL-Manager`
- Branche : `dev`
- Commit : `04bda3079becab9ec8384ed06c6ed77b3e512d65`
- Upstream au début de la revue : `origin/dev`, au même commit
- Version lue dans le code et les métadonnées : `1.2.3`
- Archive examinée : `WOL-Manager-revue-complete.zip`

L'archive a été extraite dans un répertoire temporaire après contrôle des
chemins. Elle a servi de proposition, pas de source d'autorité. Chaque ajout a
été confronté au code, à `cmd/wol/help.go`, aux handlers et à
`api/openapi.yaml`.

## 2. Inventaire des fonctionnalités publiques

| Domaine | Fonctionnalités publiques | Référence principale | Public visé |
|---|---|---|---|
| Installation | contrôle, installation AMD64/ARM64, trois modes systemd, désinstallation, migration et rollback | `install.sh` | débutant/administrateur |
| Configuration | assistant complet et assistants réseau, machine, groupe, NUT, découverte | `cmd/wol/setup.go`, `config.go` | débutant/administrateur |
| Réveil | machine, groupe, toutes les machines, simulation, dépendances | `main.go`, `engine.go` | utilisateur |
| Vérification | ICMP, plusieurs ports TCP, ARP, logique `any`/`all`, délais et annulation | `check.go`, `engine.go` | utilisateur avancé |
| Réseau | broadcast local, broadcast dirigé autorisé, relais mTLS, diagnostics et découverte bornée | `network.go`, `api.go` | administrateur |
| Groupes | priorités, dépendances, ordre, mode séquentiel/parallèle et politiques d'échec | `config.go`, `automation.go` | administrateur |
| UPS/NUT | lecture NUT, protection avant réveil et reprise persistante après coupure observée | `nut.go`, `automation.go` | administrateur |
| Tâches | états persistants, annulation, reprise/revalidation | `jobs.go` | utilisateur avancé/API |
| API | 13 routes authentifiées, permissions, limitation de débit et tâches asynchrones | `api.go`, `api/openapi.yaml` | intégrateur |
| Jetons | création, liste sans secret/hash, révocation immédiatement applicable | `main.go`, `api.go` | administrateur |
| Mise à jour | canaux `main`/`dev`, manifeste signé, SHA-256, migration et rollback | `update.go`, `install.sh` | administrateur |
| Relais | service HTTPS mTLS et contrôle de disponibilité | `api.go`, `network.go` | administrateur avancé |
| Ergonomie | menu, aides contextuelles, couleurs désactivables et autocomplétion Bash | `help.go`, `presentation.go`, `setup.go` | tous |

## 3. Correspondance fonctions/documents

| Fonction | Document principal | Documents complémentaires |
|---|---|---|
| Première installation | `docs/premiers-pas.md` | `docs/installation.md`, `README.md` |
| Référence CLI | `docs/cli.md` | aides intégrées |
| Configuration | `docs/configuration.md` | `configs/wol.example.yaml` |
| API et authentification | `docs/api.md` | `docs/security.md`, `api/openapi.yaml` |
| Réseaux/VLAN/relais | `docs/multi-vlan.md` | `docs/wol-broadcast-and-migration.md` |
| NUT et reprise | `docs/nut.md` | `docs/architecture.md`, `docs/diagnostics.md` |
| Mise à jour/rollback | `docs/updates.md` | `docs/installation.md` |
| Diagnostic | `docs/diagnostics.md` | `docs/security.md` |
| Architecture/publication | `docs/architecture.md` | `docs/development.md` |

Les documents historiques `docs/remediation-audit-2026-11-02.md` et
`docs/test-handoff-audit-2026-11-02.md` ont été conservés sans modification.

## 4. Anomalies détectées

### Critiques

Aucune anomalie documentaire critique confirmée.

### Majeures

1. Le guide API proposé ne décrivait pas chaque endpoint avec son corps, ses
   erreurs et son comportement asynchrone. Il pouvait laisser confondre
   `202 Accepted`, paquet demandé et réveil observé. Corrigé dans
   `docs/api.md`.
2. Plusieurs commandes d'administration étaient présentées sans `sudo`, alors
   que les permissions installées protègent les écritures. Corrigé dans
   `docs/cli.md` et le guide débutant.
3. Les chemins documentés des journaux d'installation et des sauvegardes
   étaient respectivement `/var/log/wol-manager/` et
   `/var/lib/wol-manager/backups/`, au lieu de
   `/var/log/wol-manager-installer/` et
   `/var/lib/wol-manager-installer/backups/`. Corrigé.
4. La description du seuil bas NUT pouvait laisser croire que toutes les
   coupures courtes seraient ignorées. Le code arme toujours sur `LB` ou `FSD`;
   seul un épisode `OB` restant au-dessus d'un seuil bas non nul peut être
   ignoré. Corrigé dans `docs/nut.md`.
5. Écart non résolu car hors périmètre autorisé : `api/openapi.yaml` annonce
   `https://127.0.0.1:8080/api/v1`, alors que la configuration et le serveur
   utilisent HTTP sur loopback par défaut. Le guide décrit le comportement
   réel ; OpenAPI devra faire l'objet d'une correction de contrat séparée.
6. Écart non résolu car hors périmètre autorisé : OpenAPI n'énumère pas toutes
   les réponses communes réellement possibles (`401`, `403`, `405`, `429`) ni
   toutes les erreurs spécifiques des handlers. Les handlers ont été
   documentés sans modifier OpenAPI.

### Mineures

- `wol network test [ID]` ne reflétait pas l'acceptation de plusieurs machines.
- Le menu était décrit comme un « réveil confirmé », formulation ambiguë
  remplacée par confirmation utilisateur avant réveil réel.
- Plusieurs liens internes étaient écrits comme des chemins de dépôt au lieu
  de liens Markdown relatifs.
- La séparation entre documentation utilisateur et contributeur n'était pas
  explicite.

### Améliorations

- Ajout d'un parcours débutant sans édition YAML.
- Ajout d'un diagnostic ordonné et de l'explication des niveaux de preuve.
- Ajout d'une matrice des 35 aides CLI vérifiées.
- Ajout d'un tableau de choix des transports et d'un scénario NUT pratique.
- Restructuration de l'index documentaire du README par profil.

## 5. Corrections proposées et appliquées

Seuls des fichiers Markdown ont été modifiés ou ajoutés. Les contenus de
l'archive ont été repris uniquement lorsqu'ils correspondaient au code réel.
Les passages incomplets ou inexacts ont été corrigés, notamment la référence
API, les privilèges CLI et le comportement NUT.

## 6. Avant/après par fichier

| Fichier | Avant | Après |
|---|---|---|
| `README.md` | liste documentaire non hiérarchisée | parcours débutant, avancé et contributeur |
| `docs/premiers-pas.md` | absent | première installation, simulation et preuves |
| `docs/cli.md` | commandes principales, privilèges partiellement implicites | syntaxe administrative corrigée, options, alias et 35 aides inventoriées |
| `docs/api.md` | table de routes sommaire | référence des 13 endpoints, permissions, corps, curl, réponses, erreurs et asynchronisme |
| `docs/installation.md` | options accessibles seulement par l'aide ; mauvais chemin de journal | contrôle préalable, options résumées et chemin exact |
| `docs/configuration.md` | référence avancée | parcours débutant et distinction héritage/absence de contrôle |
| `docs/nut.md` | seuil bas ambigu | comportement `OB`/`LB`/`FSD` et scénario de diagnostic |
| `docs/updates.md` | mauvais répertoire de sauvegarde | chemin exact et contrôles avant/après |
| `docs/multi-vlan.md` | règles de transport sans aide au choix | tableau par topologie et diagnostic sans émission |
| `docs/wol-broadcast-and-migration.md` | migration technique | procédure de contrôle post-migration |
| `docs/diagnostics.md` | diagnostics spécialisés | parcours ordonné et niveaux de preuve |
| `docs/security.md` | principes de sécurité | mise en service API et moindre privilège |
| `docs/architecture.md` | architecture interne dense | lecture simplifiée et public visé |
| `docs/development.md` | compilation/publication | frontière utilisateur/contributeur |

## 7. Contrôles réalisés

| Contrôle | Résultat |
|---|---|
| Branche, HEAD, upstream et état initial | réussi ; `dev` propre au commit indiqué |
| Inspection sûre et comparaison de l'archive | réussie |
| `wol --help` et `wol -h` | réussis, code 0 |
| 35 aides de commandes/sous-commandes | réussies, code 0 pour chacune |
| Comparaison avec `cmd/wol/help.go` | réussie ; options et alias reportés |
| Inventaire des 13 routes et handlers | réussi |
| `go mod verify` | réussi : tous les modules vérifiés |
| `go test -count=1 ./...` | réussi |
| `go vet ./...` | réussi |
| Liens Markdown relatifs | réussi sur README et 16 documents Markdown |
| `git diff --check` | réussi ; aucun défaut de diff |
| Audits historiques et OpenAPI inchangés | réussi |
| Contrôle du périmètre | réussi ; aucun code, test, script, YAML ou OpenAPI modifié |

## 8. Contrôles non exécutés

- Aucun appel API réel : la mission porte sur la conformité documentaire et
  ne demande ni déploiement ni jeton réel.
- Aucun Magic Packet, scan actif, commande NUT électrique ou modification
  système n'a été exécuté.
- Aucun parcours matériel ARM64/NUT : sans rapport avec les modifications
  Markdown et non nécessaire pour vérifier les contrats documentés.
- Aucun rendu GitHub distant : aucun push ou publication n'est autorisé.

## 9. Fichiers à publier après validation

```text
README.md
RAPPORT-VALIDATION-DOCUMENTATION.md
docs/api.md
docs/architecture.md
docs/cli.md
docs/configuration.md
docs/development.md
docs/diagnostics.md
docs/installation.md
docs/multi-vlan.md
docs/nut.md
docs/premiers-pas.md
docs/security.md
docs/updates.md
docs/wol-broadcast-and-migration.md
```

Ne pas publier le répertoire temporaire d'extraction de l'archive.

## 10. Confirmation de périmètre

Aucun fichier Go, test, script d'installation, configuration, aide CLI
intégrée, contrat OpenAPI, artefact de distribution ou document historique
d'audit n'a été modifié. Aucun commit, push, tag, merge ou publication n'a été
effectué.
