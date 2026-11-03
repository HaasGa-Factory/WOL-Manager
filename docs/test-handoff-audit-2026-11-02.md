# Transmission indépendante à l'agent TEST

## Périmètre et interdictions

Tester une copie propre de la branche `dev` et noter son commit exact. Ne modifier aucun fichier
source. Ne pas utiliser le NUT de production, ne pas déployer sur le Raspberry Pi de production et ne
pas lancer de réveil réel sans une autorisation distincte. Tous les premiers tests doivent utiliser
les simulations fournies.

## Préparation du toolchain

Installer temporairement Go 1.26.10 depuis `https://go.dev/dl/` et vérifier :

```sh
echo 'ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5  go1.26.10.linux-amd64.tar.gz' | sha256sum -c -
go env GOVERSION
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
govulncheck -version
```

Attendu : `go1.26.10` et `Scanner: govulncheck@v1.9.0`.

## Contrôles sans émission réelle

```sh
git branch --show-current
git status --short
go mod verify
go test -count=1 ./...
go vet ./...
govulncheck ./...
bash -n install.sh scripts/build.sh tests/install_test.sh
bash tests/install_test.sh
```

Attendu : branche `dev`, modules vérifiés, tests/vet sans erreur, aucun résultat govulncheck et tous
les scénarios simulés de l'installateur réussis. Les lignes `ERREUR audit` des tests négatifs sont
attendues uniquement lorsque le test provoque volontairement une panne d'écriture.

Compiler dans un répertoire temporaire, sans signer ni publier :

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -o /tmp/wol-linux-amd64 ./cmd/wol
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -o /tmp/wol-linux-arm64 ./cmd/wol
govulncheck -mode=binary /tmp/wol-linux-amd64
govulncheck -mode=binary /tmp/wol-linux-arm64
go version -m /tmp/wol-linux-amd64
go version -m /tmp/wol-linux-arm64
```

Attendu : ELF statiques des deux architectures, `go1.26.10`, aucun résultat de vulnérabilité.

## Cas d'acceptation automatisés

Vérifier dans la sortie des tests les cas suivants :

- ancienne configuration mixte refusée en lecture puis migrée seulement après confirmations, avec
  sauvegarde ;
- configuration unicast seule signalée comme incompatible ;
- broadcast local résolu vers l'interface, l'IPv4 source et le broadcast dirigé du sous-réseau ;
- interface/CIDR invalide ou réseau non autorisé : aucun appel d'envoi ;
- `--dry-run`, machine déjà joignable, absence de réponse et échec UDP ;
- distinction `socket_accepted`, `packet_emitted`, `wake_verified` ;
- dépendances, groupes, logique ICMP/TCP/ARP et politiques UPS sans régression ;
- principal API conservé pour groupe/all et action refusée si l'audit est indisponible ;
- contrôle NUT ESC/BEL simulé sans séquence de contrôle brute ;
- TTL/plafonds des jobs, rate limiter et requêtes relais ;
- installation, mise à jour, rollback et suppression de la règle logrotate.

## Validation de laboratoire optionnelle et autorisée séparément

Sur un VLAN de test uniquement, avec autorisation d'émission : utiliser une interface explicitement
déclarée, capturer le trafic sur un second point d'observation et réveiller une machine de test. Les
critères sont : destination égale au broadcast du sous-réseau, source égale à l'IPv4 de l'interface
autorisée, aucun paquet unicast, `packet_emitted` restant `unverified` tant que l'application ne reçoit
pas une preuve instrumentée, puis `wake_verified: true` seulement après réussite des contrôles. Tester
séparément machine éteinte, déjà allumée, ARP absent et vérification expirée.

## Relais mTLS

Utiliser uniquement une CA et des certificats factices. Confirmer qu'une chaîne non approuvée est
refusée et que les cibles hors du réseau affecté au relais sont refusées. Le point
`relay-mtls-peer-not-bound-to-relay-authority` n'est pas clos : documenter si la PKI réelle partage une
CA cliente entre plusieurs autorités et quelle identité applicative doit être liée au certificat. Ne
pas inventer de règle CN/SAN pendant le test.

## Critères de sortie

Acceptation si tous les contrôles non destructifs passent, si aucune référence opérationnelle à
`unicast` ne subsiste, si aucune émission n'a lieu durant les tests simulés, si les rapports de statut
ne confondent jamais socket, émission et disponibilité, et si les trois scans govulncheck sont vides.
Tout écart doit inclure commande, sortie complète, commit, architecture et environnement ; l'agent
TEST ne corrige pas le code.
