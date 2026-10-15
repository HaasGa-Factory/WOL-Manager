# Changelog

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
