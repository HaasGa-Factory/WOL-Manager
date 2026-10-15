# Compilation et publication locale

La compilation est réservée au développeur, sous Linux ou WSL. Aucun client Debian ne compile WoL Manager.

## Préparation unique de la clé

La clé publique approuvée est `packaging/release-public.pem`. Sa clé privée correspondante doit rester hors de Git, sauvegardée dans un stockage protégé et fournie par `WOL_SIGNING_KEY` ou `--signing-key`. Le chemin local `.keys/release-private.pem` est ignoré par Git.

## Cycle dev

1. Développer et exécuter les tests localement.
2. Enregistrer le code source dans un commit propre sur `dev`.
3. Compiler et signer :

   ```sh
   ./scripts/build.sh --channel dev
   ```

4. Vérifier `dist/`, `SHA256SUMS`, le manifeste et les deux formats ELF.
5. Enregistrer `dist/` dans un second commit de publication puis pousser `dev` après autorisation.
6. Tester sur Debian : `sudo wol update --channel dev`.
7. Après validation et autorisation, fusionner `dev` vers `main`, reconstruire avec `--channel main` depuis le commit source propre attendu, puis enregistrer le nouveau `dist/`.

Le commit source et le commit de publication sont volontairement distincts. Le premier est injecté dans les binaires et signé dans le manifeste ; le second contient ces artefacts et est résolu par les clients avant téléchargement.

## Contrôles de `build.sh`

Le script exige `go`, `git`, `sha256sum`, `file` et `openssl`, refuse un arbre source sale hors `dist/`, vérifie les modules, exécute `go test ./...`, compile avec `CGO_ENABLED=0` pour Linux AMD64 et ARM64, contrôle les formats ELF, signe le manifeste Ed25519 et génère `SHA256SUMS`.

Il n'effectue jamais de commit, push, merge, création de branche, GitHub Release ou publication. Le format ARM64 est contrôlé localement ; son exécution doit être validée sur une vraie machine ARM64.
