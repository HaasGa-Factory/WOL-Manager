# Rapport de remédiation de l'audit du 02.11.2026

Référence : branche `dev`, commit audité `7795a64450407f71252c7a07ad634d81e1a5af6a`.
État analysé avant correction : `dev` était exactement ce commit ; aucune correction ultérieure
n'était déjà présente. Ce rapport conserve la distinction entre défaut confirmé, point
`needs_validation` et point rejeté par l'audit.

## Matrice des 15 constats

| Fingerprint | Statut initial | Validation et décision | Correctif / fichiers principaux | Tests | Risque résiduel |
|---|---|---|---|---|---|
| `wol-unicast-kernel-acceptance-blocks-broadcast-fallback` | confirmed | Défaut reproduit par l'audit ; décision fonctionnelle de supprimer l'unicast. | Suppression de la sélection/fallback, broadcast local lié à interface/source, migration confirmée et nouveaux statuts. `config.go`, `network.go`, `engine.go`, `setup.go`, docs et tests. | Plan broadcast, échec UDP, dry-run, migration mixte/seule, vérifications et suite complète. | `packet_emitted` reste `unverified` sans capture indépendante, par conception. |
| `release-build-toolchain-unpinned-vulnerable-stdlib` | needs_validation | Le binaire audité était Go 1.24.4 ; le scan d'audit recensait des vulnérabilités stdlib dont le plancher maximal de correction était Go 1.26.10. Validation avec ce toolchain : aucun résultat source ou binaire. | Épinglage Go 1.26.10 et govulncheck v1.9.0, contrôles source + deux binaires, provenance dans manifeste. `scripts/build.sh`, `docs/development.md`. | `go mod verify`, `go test -count=1 ./...`, `go vet ./...`, trois scans govulncheck, builds AMD64/ARM64. | Une nouvelle publication doit être reconstruite depuis un commit propre ; les anciens binaires `dist/` n'ont pas été remplacés dans cette mission. |
| `api.bulk-wake-audit-principal-omitted` | needs_validation | Perte d'identité confirmée dans les jobs groupés/globaux. | Principal persisté dans le job et propagé au contexte d'exécution pour wake, groupe, all et découverte. `api.go`, `jobs.go`, `engine.go`, OpenAPI. | `TestBulkWakeJobKeepsAuthenticatedPrincipal`, persistance/reprise d'acteur. | Les anciens jobs déjà persistés n'ont pas d'acteur historique. |
| `audit-write-errors-silently-discarded` | needs_validation | L'échec d'ouverture/écriture était ignoré. Risque confirmé. | `Auditor.write` retourne et journalise les erreurs. Écriture `requested` fail-closed avant réveil, automatisme et découverte active ; API 503 sans job. `audit.go`, `api.go`, `engine.go`, `automation.go`, `main.go`. | Panne de répertoire simulée et refus API/engine. | Une erreur sur un événement terminal ne peut pas annuler une action déjà effectuée ; elle est visible dans le journal du processus. |
| `audit.unbounded-jsonl-retention` | needs_validation | Absence de politique source confirmée. | Règle logrotate installée : hebdomadaire, 12 archives, maxsize 10 MiB, compression, création 0640. `packaging/logrotate/wol-manager`, `install.sh`. | Installation, mise à jour, rollback et désinstallation simulés. | Dépend de logrotate sur l'hôte ; à valider sur VM Debian réelle. |
| `nut-terminal-control-sequence-injection` | needs_validation | Chemin de rendu externe confirmé avec reproduction inoffensive simulée. | Neutralisation des C0/C1 en séquences visibles à la frontière de parsing NUT. `nut.go`. | ESC et BEL simulés, limites NUT existantes. | Les données restent non fiables sémantiquement, mais ne contiennent plus de contrôle terminal brut. |
| `jobs.unbounded-terminal-retention` | needs_validation | Croissance persistante confirmée. | TTL 30 jours et plafond 1 000 jobs terminaux ; actifs/revalidation préservés. `jobs.go`. | Expiration, plafond et préservation des jobs actifs. | Un résultat individuel peut encore être volumineux dans les limites des opérations existantes. |
| `api-rate-cleanup-linear-scan-under-global-lock` | needs_validation | Scan jusqu'à 4 096 entrées effectué à chaque requête. | Nettoyage au plus une fois par fenêtre d'une minute, cardinalité existante conservée à 4 096. `api.go`. | Fenêtres, plafond, entrées actives et concurrence. | Le nettoyage périodique reste O(4 096), mais sa fréquence et son coût sont bornés. |
| `relay.seen-request-id-unbounded-cardinality` | needs_validation | Cardinalité 24 h non bornée confirmée. | Plafond 4 096, TTL 24 h, identifiant max 128 octets, refus 429 à capacité. `api.go`. | Remplissage borné, doublon, dépassement et expiration. | Saturation volontaire provoque un refus temporaire, comportement fail-closed. |
| `relay-mtls-peer-not-bound-to-relay-authority` | needs_validation | Le certificat est validé par CA, sans politique applicative de sujet ; aucune autorité attendue n'est définie dans la configuration/protocole. | Pas de changement protocolaire arbitraire. Documentation d'une CA cliente dédiée par relais et maintien des restrictions de cible. `docs/security.md`. | Certificats CA/client factices existants ; chargement mTLS et restrictions de réseau. | Conditionnel si une CA est partagée entre autorités. Une politique d'identité explicite doit être définie avant correction applicative. |
| `config.privileged-editor-environment-confused-deputy` | rejected | L'audit n'a pas démontré une délégation sudo restreinte exploitable. | Aucune intervention. | Tests CLI/config existants. | Risque général d'une politique sudo mal conçue, hors défaut applicatif démontré. |
| `deployment.secret-files-permissions-unverified` | rejected | Les preuves de déploiement n'ont pas confirmé de permissions dangereuses. | Aucune modification de secrets ou déploiement. | Tests installateur des modes 0600/0640. | À recontrôler sur chaque hôte de production, sans modifier les secrets. |
| `installer.test-mode-environment-preserved-by-sudo` | rejected | Les préconditions d'exploitation n'étaient pas établies par l'audit. | Aucune modification fonctionnelle. | Suite complète de l'installateur simulé. | Une configuration sudoers externe dangereuse reste hors périmètre. |
| `relay.client-certificate-revocation-not-enforced` | rejected | Aucun mécanisme de révocation promis ou contourné n'a été démontré. | Aucune nouvelle PKI/CRL introduite. | Tests mTLS factices existants. | Rotation/révocation relève de la PKI de déploiement actuelle. |
| `tls.private-key-permissions-not-enforced` | rejected | L'audit n'a pas démontré de clé lisible par un acteur non autorisé. | Aucune modification des clés/certificats. | Installation et permissions simulées. | Vérification opérationnelle requise sur les hôtes, sans exposer le contenu des clés. |

## Résultats techniques

- Go : `go1.26.10`, archive Linux AMD64 dont le SHA-256 officiel
  `ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5` a été vérifié.
- Scanner : `govulncheck@v1.9.0`, base mise à jour le 2026-11-02 22:01:43 UTC.
- Modules : `go mod verify` réussi ; aucune mise à jour de dépendance applicative.
- Tests : `go test -count=1 ./...` réussi avec Go 1.26.10 ; `go vet ./...` réussi.
- Vulnérabilités : `govulncheck ./...`, AMD64 binaire et ARM64 binaire : « No vulnerabilities found ».
- Builds de validation temporaires : ELF statiques linux/amd64 et linux/arm64, tous deux marqués
  `go1.26.10`. Aucun artefact n'a été copié dans `dist/`, signé ou publié.
- Installateur : les 16 scénarios et compléments simulés ont réussi, y compris rotation, rollback et
  désinstallation.
- Réseau/NUT : aucun réveil physique, aucune commande de contrôle UPS, aucun déploiement et aucun
  contact avec la production.

## Commits et rollback

Le commit de correction est `813dc5018cc45400ccbd01d9326ee0d3405df6db`. Le commit documentaire
qui contient les trois rapports de remise est volontairement séparé. `.codex-attachments/`, `dist/`,
les secrets et les artefacts temporaires ont été exclus. Aucun push, tag, artefact signé ou release
n'a été créé.

Le rollback source consiste à revenir aux commits de correction après validation Git ; le rollback
de configuration utilise la sauvegarde datée produite par `wol setup`. Une publication ultérieure
doit être reconstruite depuis le commit source propre avec `scripts/build.sh --channel dev`, puis
testée indépendamment avant tout déploiement.
