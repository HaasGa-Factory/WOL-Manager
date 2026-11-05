# Mises à jour et rollback

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
anciens champs de transport et conserve une sauvegarde datée. L'état activé/désactivé du service est
conservé. Une tâche interrompue reste soumise à la revalidation du moteur existant.

## Installation atomique et rollback

Avant remplacement, l'installateur valide le nouveau binaire et la
configuration, puis sauvegarde le binaire, le service, l'installateur et les
métadonnées sous `/var/lib/wol-manager-installer/backups/`. Le remplacement du
binaire utilise un fichier temporaire sur le même système de fichiers.

Si le lancement, la validation YAML ou le redémarrage systemd échoue, ces composants sont restaurés et l'ancien service est relancé lorsque possible. Aucune migration irréversible de données n'est exécutée dans cette Beta ; la migration confirmée de la configuration conserve sa sauvegarde datée et les données persistantes restent intactes. Le journal indique clairement si le rollback a été déclenché.

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
`wol config check` et `wol network test`.
