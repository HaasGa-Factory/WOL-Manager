# Mises à jour et rollback

## Migration depuis un ancien installateur vers une publication complète

Les installateurs système livrés avec `1.2.1`, `1.2.3` et `1.3.0` confondent
une configuration invalide avec une configuration valide mais temporairement
non opérationnelle (`valid: true`, `ready: false`). Ils téléchargent et
authentifient le nouveau binaire, puis s'arrêtent avant de remplacer leur
propre moteur. Le nouvel installateur corrigé ne peut donc pas prendre le
relais par la seule commande `wol update`.

Plus généralement, les installateurs jusqu'à `1.4.5` exécutent la migration
avec la logique de la version de départ. Le nouvel `install.sh` signé est bien
copié, mais il ne pilote que l'exécution suivante. Une mise à jour peut alors
laisser un ancien modèle systemd ou omettre Logrotate si cette ressource
n'existait pas dans l'ancien agencement.

Le bootstrap
[`upgrade-network-validation.sh`](../scripts/migration/upgrade-network-validation.sh)
résout ces cas. Il accepte les versions `1.2.1`, `1.2.3`, `1.3.0` et `1.4.x`
jusqu'à `1.4.5`, vérifie le manifeste
officiel avec la clé Ed25519 déjà installée, contrôle le SHA-256 de
`dist/install.sh`, sauvegarde l'ancien installateur et remplace atomiquement sa
copie système. Il ne modifie ni le binaire `wol`, ni `wol.yaml`, ni les secrets,
ni les états, ni les services.

Téléchargez le script depuis le **commit de publication exact** de `1.4.5`, et
non depuis une archive ou une copie non identifiée. Les commandes suivantes
résolvent d'abord la branche `dev` vers son commit complet, puis épinglent le
contenu attendu du script avec son SHA-256 :

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
```

Le nom historique du script est conservé pour ne pas casser les procédures déjà
publiées, mais son rôle est désormais d'amorcer l'installateur complet.

Le contrôle doit afficher `upgrade-network-validation.sh: OK`. Inspectez ensuite
le fichier téléchargé, puis effectuez d'abord la simulation :

```sh
sudo ./upgrade-network-validation.sh --channel dev --dry-run
sudo ./upgrade-network-validation.sh --channel dev
sudo wol update --channel dev
```

Pour une publication stable, utilisez le commit de publication `main` et
remplacez `--channel dev` par `--channel main`. Le script refuse un manifeste
d'un autre canal, une signature invalide, un installateur altéré, une version
candidate antérieure à `1.4.5` ou une version installée hors de la liste prise
en charge. Un second passage est idempotent.

Après la mise à jour :

```sh
wol version
wol config check
wol network test IDENTIFIANT_MACHINE
```

À la fin, le contrôle de conformité classe chaque composant en `OK`,
`Désactivé volontairement`, `AVERTISSEMENT` ou `ERREUR`. Logrotate,
l'autocomplétion, les modèles systemd, les permissions et les ressources
embarquées sont comparés à la version cible. Une ressource obligatoire absente
provoque un échec et un rollback au lieu d'un succès partiel.

`wol config check` peut légitimement retourner le code `1` avec
`valid: true, ready: false` si un réseau distant n'a aucun transport disponible.
La mise à jour reste installée, mais les réveils concernés restent bloqués. Les
Même si les installations `1.4.1` et ultérieures connaissent déjà cette
distinction, utilisez aussi ce bootstrap lorsqu'un contrôle signale une
ressource système absente ou obsolète.

## Mise à jour 1.4.3 vers 1.4.4

La version `1.4.4` corrige les fins de ligne Windows `CRLF` qui peuvent rendre
`/etc/logrotate.d/wol-manager` illisible par Logrotate. La configuration YAML,
les états persistants, `audit.jsonl` et ses archives ne sont pas modifiés.

Une installation déjà en `1.4.3` doit exécuter la commande deux fois : le
premier passage installe le binaire et le nouvel installateur signé ; le second
passage, désormais piloté par cet installateur, normalise la ressource et le
fichier système même si aucune nouvelle version n'est disponible.

```sh
sudo wol update --channel dev
sudo wol update --channel dev
```

Vérifiez ensuite l'absence de `\r` et la reconnaissance du journal :

```sh
sudo sed -n '1,15l' /etc/logrotate.d/wol-manager
sudo logrotate -d /etc/logrotate.d/wol-manager
```

La première commande ne doit afficher aucun `\r$`. La seconde doit annoncer
`Handling 1 logs` sans erreur de syntaxe. Cette procédure ne nécessite ni
`dos2unix`, ni suppression, ni rotation forcée du journal.

## Mise à jour 1.4.2 vers 1.4.3

Cette mise à jour utilise le parcours standard :

```sh
wol update --check --channel dev
sudo wol update --channel dev
```

L'installateur conserve le contenu de
`/var/log/wol-manager/audit.jsonl`, puis normalise automatiquement le journal
en `wol-manager:wol-manager` avec le mode `0640`. Aucune copie manuelle du
journal ni modification de la configuration YAML n'est nécessaire.

Après la mise à jour :

```sh
sudo systemctl restart wol-manager.service
sudo stat -c '%U:%G %a %n' /var/log/wol-manager/audit.jsonl
sudo journalctl -u wol-manager.service -n 100 --no-pager
```

Le fichier doit être annoncé comme `wol-manager:wol-manager 640`. Les
événements importants du daemon sont visibles dans journald, tandis que
`audit.jsonl` conserve la trace JSON structurée.

## Canal principal

```sh
sudo wol update
```

La commande résout la branche `main` vers un commit GitHub précis, vérifie le manifeste signé conservé dans `dist/`, compare la version et la révision source, puis demande confirmation avant tout remplacement. Le manifeste de format 2 couvre également `dist/install.sh`, ce qui permet de mettre à jour atomiquement le moteur d'installation sans exécuter un script non signé.

```sh
wol update --check
```

La vérification ne modifie rien et ne demande pas de privilèges administrateur.

Dans un terminal interactif, l'installateur colore les avertissements DEV/BETA, les erreurs, les vérifications réussies, la comparaison des versions et la confirmation avant installation. Les fichiers de journal restent toujours en texte brut. Définissez `NO_COLOR=1` pour désactiver les couleurs ; elles sont également désactivées automatiquement lorsque la sortie est redirigée.

## Canal de développement

```sh
sudo wol update --channel dev
```

Un avertissement BETA/DEV est affiché. Ce choix vaut uniquement pour cette commande : l'exécution suivante de `sudo wol update` revient automatiquement à `main`.

Une configuration peut être syntaxiquement valide (`valid: true`) tout en étant non opérationnelle
(`ready: false`), par exemple lorsqu'aucun transport WoL n'est utilisable. Cet état ne bloque pas
la mise à jour et le YAML n'est pas modifié. À partir de `1.4.2`, le diagnostic regroupe les machines
qui partagent le même défaut afin de ne pas répéter la correction pour chaque serveur. Il indique le
réseau concerné, les transports refusés et les commandes exactes à exécuter. Par exemple :

```text
CONFIGURATION VALIDE, MAIS NON OPÉRATIONNELLE

RÉSEAU À CORRIGER : lan

Machines concernées
  - home_assistant
  - proxmox
  - truenas

Transports examinés
  - Broadcast local : interface eth0 indisponible
  - Broadcast dirigé : autorisation explicite absente
  - Relais : aucun relais configuré

CORRECTION RECOMMANDÉE
  1. Identifiez l’interface IPv4 actuellement utilisée :
       ip -br -4 address
  2. Ouvrez l’assistant :
       sudo wol setup
  3. Sélectionnez Réseaux WoL -> Modifier -> lan.
```

Après la correction, contrôlez chaque machine indiquée et l'ensemble de la configuration :

```sh
wol network test IDENTIFIANT_MACHINE
wol config check
```

La sortie JSON de `wol --json config check` conserve les champs `valid`, `ready`, `version` et
`issues`. Une validation humaine ou JSON non opérationnelle retourne toujours le code `1`.

### Amorçage de l'installateur 1.4.1 vers 1.4.2

L'installateur système `1.4.1` reconnaît l'état `ready: false` dans l'ancienne sortie JSON de
`wol config check`. La version `1.4.2` affiche désormais le rapport humain structuré ci-dessus.
Si la configuration installée n'est pas encore opérationnelle, l'ancien installateur ne reconnaît
pas ce nouveau format et peut arrêter la mise à jour avec :

```text
ERREUR: Configuration incompatible avec 1.4.2
```

Dans ce cas seulement, amorcez l'installateur `1.4.2` signé avant de relancer la mise à jour. Les
commandes suivantes résolvent `dev` vers un commit précis, puis téléchargent le manifeste, sa
signature et l'installateur depuis ce même commit :

```sh
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
cd "$work"

publication=$(curl -fsSL \
  https://api.github.com/repos/HaasGa-Factory/WOL-Manager/commits/dev \
  | sed -n 's/^[[:space:]]*"sha":[[:space:]]*"\([0-9a-f]\{40\}\)".*/\1/p' \
  | head -n1)

printf '%s\n' "$publication" | grep -Eq '^[0-9a-f]{40}$' || {
  echo "ERREUR: commit de publication dev introuvable" >&2
  exit 1
}

base="https://raw.githubusercontent.com/HaasGa-Factory/WOL-Manager/$publication/dist"
curl -fSLo manifest.json "$base/manifest.json"
curl -fSLo manifest.json.sig "$base/manifest.json.sig"
curl -fSLo install.sh "$base/install.sh"

openssl base64 -d -A -in manifest.json.sig -out manifest.sig
openssl pkeyutl -verify -rawin -pubin \
  -inkey /usr/local/lib/wol-manager/packaging/release-public.pem \
  -in manifest.json -sigfile manifest.sig

grep -Fq '"version": "1.4.2"' manifest.json &&
grep -Fq '"channel": "dev"' manifest.json || {
  echo "ERREUR: le canal dev ne publie pas encore 1.4.2" >&2
  exit 1
}

expected=$(sed -n \
  's/^[[:space:]]*"installer_sha256"[[:space:]]*:[[:space:]]*"\([0-9a-f]*\)".*/\1/p' \
  manifest.json)
actual=$(sha256sum install.sh | awk '{print $1}')
[ -n "$expected" ] && [ "$actual" = "$expected" ] || {
  echo "ERREUR: install.sh ne correspond pas au manifeste signé" >&2
  exit 1
}

sudo install -o root -g root -m 0755 \
  install.sh \
  /usr/local/lib/wol-manager/install.sh

sudo /usr/local/lib/wol-manager/install.sh --version
sudo wol update --channel dev
```

Cette procédure ne remplace que le moteur d'installation après vérification Ed25519 et SHA-256.
Elle ne modifie ni le binaire `wol`, ni le YAML, ni les secrets, ni l'état persistant. La dernière
commande reprend la mise à jour normale ; le nouveau rapport explique ensuite les transports à
corriger sans bloquer l'installation.

Une machine encore équipée de l'installateur système `1.3.0` peut toutefois
s'arrêter avant d'installer cette correction. Suivez alors la
[procédure d'amorçage 1.3.0 vers 1.4.1](../README.md#mise-à-niveau-130-vers-141),
qui authentifie le nouvel installateur avant de remplacer uniquement sa copie
système.

### Amorçage depuis l'installateur 1.2.1

L'installateur système `1.2.1` télécharge et authentifie bien les fichiers `1.2.3`, mais il contrôle
la configuration avec le nouveau binaire avant de remplacer son propre moteur. Une configuration
contenant encore `unicast` provoque donc « Configuration incompatible » avant que la migration
guidée de `1.2.3` puisse démarrer.

Cette transition demande une seule copie locale du nouvel installateur. Placez-vous à la racine du
paquet `1.2.3` téléchargé, puis vérifiez son manifeste avec la clé publique déjà installée :

```sh
signature=$(mktemp)
openssl base64 -d -A -in dist/manifest.json.sig -out "$signature"
openssl pkeyutl -verify -rawin -pubin \
  -inkey /usr/local/lib/wol-manager/packaging/release-public.pem \
  -in dist/manifest.json -sigfile "$signature"
rm -f "$signature"

expected=$(sed -n 's/^[[:space:]]*"installer_sha256"[[:space:]]*:[[:space:]]*"\([0-9a-f]*\)".*/\1/p' dist/manifest.json)
actual=$(sha256sum dist/install.sh | awk '{print $1}')
[ -n "$expected" ] && [ "$actual" = "$expected" ] || {
  echo "ERREUR: dist/install.sh ne correspond pas au manifeste signé" >&2
  exit 1
}
```

Après le message `Signature Verified Successfully`, installez uniquement cette copie vérifiée, puis
reprenez la mise à jour normale :

```sh
sudo install -o root -g root -m 0755 \
  ./dist/install.sh \
  /usr/local/lib/wol-manager/install.sh
sudo /usr/local/lib/wol-manager/install.sh --version
sudo wol update --channel dev
```

Cette étape ne remplace ni le binaire `wol`, ni la configuration, ni les secrets. La dernière
commande ouvre ensuite la migration guidée et conserve une sauvegarde datée avant toute écriture.

Si vous avez déjà amorcé l'installateur `1.2.2` et qu'il signale l'absence de
`packaging/logrotate/wol-manager`, utilisez le fichier du paquet `dev`, jamais celui du paquet
`main` qui ne le contient pas encore. Vérifiez d'abord :

```sh
echo 'cead19ee7daf701832736c08859abe0199391d57270facc9baf65738ad87a500  packaging/logrotate/wol-manager' \
  | sha256sum -c -
```

Puis copiez-le dans l'agencement de l'installateur et relancez :

```sh
sudo install -d -o root -g root -m 0755 \
  /usr/local/lib/wol-manager/packaging/logrotate
sudo install -o root -g root -m 0644 \
  ./packaging/logrotate/wol-manager \
  /usr/local/lib/wol-manager/packaging/logrotate/wol-manager
sudo wol update --channel dev
```

### Migration guidée d'une ancienne configuration

Avec l'installateur système `1.2.3`, si la configuration utilise encore l'ancien transport `unicast`,
le nouveau binaire signé est téléchargé et vérifié mais n'est pas installé immédiatement. Il explique le
changement et propose d'ouvrir son assistant de migration. Celui-ci retire uniquement les anciens
champs `unicast` et `allow_unicast`, exige qu'un broadcast ou un relais utilisable reste configuré,
affiche le résumé puis demande confirmation avant l'écriture atomique et la sauvegarde datée.

La mise à jour ne reprend qu'après une nouvelle validation de la configuration migrée. Une annulation
conserve l'ancien binaire. En mode `--non-interactive`, aucune migration n'est effectuée : relancez
la commande dans un terminal avec `sudo wol update --channel dev`.

### Vérification de `main` depuis une version `dev`

Pendant une transition, `dev` peut utiliser le manifeste de format 2 alors que `main` publie encore le format signé 1. Dans ce cas précis, `wol update --check` et `wol update --check --channel main` vérifient d'abord la signature du manifeste, expliquent que `main` ne peut pas remplacer la version `dev`, puis s'arrêtent sans modifier le binaire, l'installateur, la configuration, les secrets, le service ou le canal enregistré. Vérifiez temporairement le canal de développement avec :

```sh
wol update --check --channel dev
```

Il faut attendre la publication de la version sur `main` avant de revenir au canal stable. Cette exception d'affichage ne s'applique qu'au format signé connu 1 pendant un contrôle sans installation : une signature incorrecte ou tout autre format incompatible reste une erreur stricte. Le canal par défaut reste `main`.

Lorsque `main` publie ensuite exactement la même version et la même révision source que le
binaire `dev` installé, le canal reste un critère de comparaison. WoL Manager propose alors le
remplacement du binaire de développement par le binaire signé stable au lieu d'annoncer à tort
que l'installation est déjà à jour.

### Transition unique depuis le manifeste de format 1

Les installations `1.1.0-beta` construites depuis la révision `0d423dd` possèdent un ancien moteur qui ne sait mettre à jour que le binaire. Pour éviter qu'il réécrive le fichier de complétion appartenant à Debian, le premier passage au manifeste de format 2 est volontairement refusé par cet ancien moteur.

Sur une machine de test disposant du paquet `dev` à jour, appliquez la même procédure de vérification
et de copie que dans « Amorçage depuis l'installateur 1.2.1 », puis reprenez la commande normale.

Les mises à jour suivantes actualisent automatiquement cet installateur depuis le même manifeste signé. Si l'ancien WoL Manager avait remplacé `/usr/share/bash-completion/completions/wol`, le nouvel installateur le signale sans modifier ce fichier. Après accord de l'administrateur, le fichier Debian peut être restauré avec :

```sh
sudo apt-get install --reinstall bash-completion
```

L'installateur choisit l'architecture et télécharge le manifeste, sa signature, l'installateur et le binaire depuis le même commit de publication. Il n'existe aucun fallback de compilation côté client. Une erreur réseau, une branche absente ou un artefact indisponible est signalé sans modifier l'installation.

Hors migration guidée explicitement confirmée, la configuration, les jetons, secrets, données,
politiques UPS, réseaux et relais ne sont pas modifiés. La migration `unicast` ne touche que les
anciens champs de transport et conserve une sauvegarde datée. L'état
activé/désactivé des services est conservé. Une tâche interrompue reste
soumise à la revalidation du moteur existant.

### Migration des services v1.2.3 vers v1.4.2

La version 1.4.0 sépare le daemon d'automatisation de l'API REST. Un ancien
`wol-manager.service` contenant `api serve` est reconnu comme unité combinée.
Le premier `wol update` lancé depuis v1.2.3 reste piloté par l'installateur
v1.2.3 déjà présent sur la machine. Cet ancien programme sait authentifier et
copier le nouveau binaire et le nouvel installateur, mais il ne connaît pas
encore les deux nouvelles unités. Il ne peut donc pas exécuter rétroactivement
leur migration pendant cette première commande.

Le nouveau binaire maintient alors temporairement l'API et le moniteur dans le
même processus. Son socket interne est créé dans un répertoire temporaire privé
et inscriptible par `wol-manager` : il ne dépend plus de la création interdite
de `/run/wol-manager` par l'ancien service.

Relancez ensuite exactement la même mise à jour. Le nouvel installateur est
désormais celui qui s'exécute ; même si le binaire est annoncé à jour, il
détecte et migre automatiquement l'unité combinée :

```sh
sudo wol update --channel dev
systemctl status wol-manager.service wol-manager-api.service
sudo wol automation status
```

Pour une publication stable, omettez `--channel dev` et relancez simplement
`sudo wol update`.

La migration sauvegarde l'ancienne unité et le binaire, préserve intégralement
la configuration, les jetons, les jobs et les cycles, ne contacte pas NUT et ne
redémarre aucun service NUT. Une ancienne unité active et activée produit deux
unités actives et activées. Une unité volontairement inactive ou désactivée
conserve exactement ces états. En cas d'échec de l'installation ou du
démarrage, l'ancienne unité, le binaire précédent et leurs états sont restaurés.
Une nouvelle exécution est idempotente.

`wol-manager.service` devient l'unique propriétaire de l'état et crée
`/run/wol-manager` avec `RuntimeDirectory=wol-manager` ;
`wol-manager-api.service` lui délègue les jobs par le socket local.

Une configuration peut être syntaxiquement valide tout en étant non
opérationnelle, par exemple lorsqu'un ancien backup référence des machines
mais que leur réseau n'autorise plus aucun transport WoL. Cet état reste
signalé par `wol config check` avec `valid: true`, `ready: false` et un code de
sortie non nul. Il ne bloque toutefois plus l'installation d'une mise à jour :
l'installateur conserve le YAML, affiche un avertissement et rappelle de
corriger ensuite les transports avec `sudo wol setup`. Une configuration
syntaxiquement invalide ou utilisant un format incompatible reste strictement
refusée.

## Installation atomique et rollback

Avant remplacement, l'installateur valide le nouveau binaire et la
configuration, puis sauvegarde le binaire, les deux unités systemd, l'installateur et les
métadonnées sous `/var/lib/wol-manager-installer/backups/`. Le remplacement du
binaire utilise un fichier temporaire sur le même système de fichiers.

Si le lancement, la validation YAML ou le redémarrage systemd échoue, ces composants sont restaurés et les anciens services sont relancés lorsque possible. Aucune migration irréversible de données n'est exécutée dans cette version ; la migration confirmée de la configuration conserve sa sauvegarde datée et les données persistantes restent intactes. Le journal indique clairement si le rollback a été déclenché.

## Intégrité et provenance

La clé publique Ed25519 approuvée est installée avec WoL Manager. Elle vérifie le manifeste avant lecture de ses champs ; les SHA-256 signés vérifient ensuite le binaire et l'installateur. La version, le canal et la révision source intégrés doivent correspondre au manifeste. La branche GitHub est d'abord résolue vers un commit de publication, afin de ne jamais mélanger des fichiers de commits différents.

Le `source_revision` désigne le commit source propre utilisé pour compiler. Le commit de publication, nécessairement ultérieur puisqu'il contient les binaires, est enregistré séparément dans `release.conf`. Cette séparation évite le problème circulaire d'un binaire contenant le hash du commit qui le contient.

## Vérification avant et après mise à jour

Avant une installation, utilisez :

```sh
wol version
wol update --check
wol config check
```

Pour vérifier temporairement le canal de développement, remplacez la deuxième
commande par `wol update --check --channel dev`. Lisez les messages de
compatibilité et ne forcez pas `--non-interactive` pour contourner une
migration refusée. Après la mise à jour, contrôlez de nouveau `wol version`,
`wol config check`, `wol network test`, `wol automation status` et l'état des
deux unités systemd si elles sont installées.
