# Compilation et publication locale

La compilation est réservée au développeur, sous Linux ou WSL. Aucun client Debian ne compile WoL Manager.

Le build de publication exige exactement Go `1.26.10` et `govulncheck@v1.9.0`. Installer ces
versions depuis leurs sources officielles et vérifier le SHA-256 de l'archive Go avant utilisation.

## Préparation unique de la clé

La clé publique approuvée est `packaging/release-public.pem`. Sa clé privée correspondante doit rester hors de Git, sauvegardée dans un stockage protégé et fournie par `WOL_SIGNING_KEY` ou `--signing-key`. Le chemin local `.keys/release-private.pem` est ignoré par Git.

## Cycle dev

1. Développer et exécuter les tests localement.
2. Enregistrer le code source dans un commit propre sur `dev`.
3. Compiler et signer :

   ```sh
   ./scripts/build.sh --channel dev
   ```

4. Vérifier `dist/`, `SHA256SUMS`, le manifeste, sa copie signée de `install.sh` et les deux formats ELF.
5. Enregistrer `dist/` dans un second commit de publication puis pousser `dev` après autorisation.
6. Tester sur Debian : `sudo wol update --channel dev`.
7. Après validation et autorisation, fusionner `dev` vers `main`, reconstruire avec `--channel main` depuis le commit source propre attendu, puis enregistrer le nouveau `dist/`.

Le commit source et le commit de publication sont volontairement distincts. Le premier est injecté dans les binaires et signé dans le manifeste ; le second contient ces artefacts et est résolu par les clients avant téléchargement.

## Contrôles de `build.sh`

Le script exige `go`, `govulncheck`, `git`, `sha256sum`, `file` et `openssl`, refuse un arbre source sale hors `dist/`, exécute `go mod verify`, `go test -count=1 ./...`, `go vet ./...` et `govulncheck ./...`, compile avec `CGO_ENABLED=0` pour Linux AMD64 et ARM64 puis analyse aussi les deux binaires avec `govulncheck -mode=binary`. Il contrôle les formats ELF, ajoute une copie exacte de `install.sh`, signe le manifeste Ed25519 et génère `SHA256SUMS`. Le manifeste enregistre le commit source, la version exacte de Go, la version du scanner et les résultats des trois scans.

Il n'effectue jamais de commit, push, merge, création de branche, GitHub Release ou publication. Le format ARM64 est contrôlé localement ; son exécution doit être validée sur une vraie machine ARM64.

## Frontière entre documentation utilisateur et développement

Ce document s'adresse aux contributeurs qui compilent et préparent une
publication. L'utilisateur final n'a pas à exécuter `build.sh`, installer Go
ni gérer les signatures : il suit [l'installation](installation.md), puis les
[premiers pas](premiers-pas.md).

Avant de publier une documentation, vérifiez les commandes citées contre
`cmd/wol/help.go`, et les routes HTTP contre les handlers et
`api/openapi.yaml`. Une revue documentaire ne modifie ni les aides intégrées,
ni le comportement du programme.
