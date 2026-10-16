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

## Canal de développement

```sh
sudo wol update --channel dev
```

Un avertissement BETA/DEV est affiché. Ce choix vaut uniquement pour cette commande : l'exécution suivante de `sudo wol update` revient automatiquement à `main`.

Lorsqu'une publication `main` reprend exactement la même version et la même révision source qu'une version `dev` installée, le canal fait partie de la comparaison. WoL Manager propose donc bien de remplacer le binaire `dev` par le binaire signé `main` au lieu d'annoncer à tort que le logiciel est déjà à jour. Le contrôle `--check` ne modifie rien et le canal par défaut reste `main`.

### Vérification de `main` depuis une version `dev`

Pendant une transition, `dev` peut utiliser le manifeste de format 2 alors que `main` publie encore le format signé 1. Dans ce cas précis, `wol update --check` et `wol update --check --channel main` vérifient d'abord la signature du manifeste, expliquent que `main` ne peut pas remplacer la version `dev`, puis s'arrêtent sans modifier le binaire, l'installateur, la configuration, les secrets, le service ou le canal enregistré. Vérifiez temporairement le canal de développement avec :

```sh
wol update --check --channel dev
```

Il faut attendre la publication de la version sur `main` avant de revenir au canal stable. Cette exception d'affichage ne s'applique qu'au format signé connu 1 pendant un contrôle sans installation : une signature incorrecte ou tout autre format incompatible reste une erreur stricte. Le canal par défaut reste `main`.

### Transition unique depuis le manifeste de format 1

Les installations `1.1.0-beta` construites depuis la révision `0d423dd` possèdent un ancien moteur qui ne sait mettre à jour que le binaire. Pour éviter qu'il réécrive le fichier de complétion appartenant à Debian, le premier passage au manifeste de format 2 est volontairement refusé par cet ancien moteur.

Sur une machine de test disposant du dépôt `dev` à jour, amorcez une seule fois le nouvel installateur local, puis reprenez la commande normale :

```sh
sudo install -o root -g root -m 0755 ./dist/install.sh /usr/local/lib/wol-manager/install.sh
sudo wol update --channel dev
```

Les mises à jour suivantes actualisent automatiquement cet installateur depuis le même manifeste signé. Si l'ancien WoL Manager avait remplacé `/usr/share/bash-completion/completions/wol`, le nouvel installateur le signale sans modifier ce fichier. Après accord de l'administrateur, le fichier Debian peut être restauré avec :

```sh
sudo apt-get install --reinstall bash-completion
```

L'installateur choisit l'architecture et télécharge le manifeste, sa signature, l'installateur et le binaire depuis le même commit de publication. Il n'existe aucun fallback de compilation côté client. Une erreur réseau, une branche absente ou un artefact indisponible est signalé sans modifier l'installation.

La configuration, les jetons, secrets, données, politiques UPS, réseaux et relais ne sont pas modifiés. L'état activé/désactivé du service est conservé. Une tâche interrompue reste soumise à la revalidation du moteur existant.

## Installation atomique et rollback

Avant remplacement, l'installateur valide le nouveau binaire et la configuration, puis sauvegarde le binaire, le service, l'installateur et les métadonnées sous `/var/lib/wol-manager/backups/`. Le remplacement du binaire utilise un fichier temporaire sur le même système de fichiers.

Si le lancement, la validation YAML ou le redémarrage systemd échoue, ces composants sont restaurés et l'ancien service est relancé lorsque possible. Aucune migration irréversible de données n'est exécutée dans cette Beta ; les données persistantes restent intactes. Le journal indique clairement si le rollback a été déclenché.

## Intégrité et provenance

La clé publique Ed25519 approuvée est installée avec WoL Manager. Elle vérifie le manifeste avant lecture de ses champs ; les SHA-256 signés vérifient ensuite le binaire et l'installateur. La version, le canal et la révision source intégrés doivent correspondre au manifeste. La branche GitHub est d'abord résolue vers un commit de publication, afin de ne jamais mélanger des fichiers de commits différents.

Le `source_revision` désigne le commit source propre utilisé pour compiler. Le commit de publication, nécessairement ultérieur puisqu'il contient les binaires, est enregistré séparément dans `release.conf`. Cette séparation évite le problème circulaire d'un binaire contenant le hash du commit qui le contient.
