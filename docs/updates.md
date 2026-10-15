# Mises à jour et rollback

## Canal principal

```sh
sudo wol update
```

La commande résout la branche `main` vers un commit GitHub précis, vérifie le manifeste signé conservé dans `dist/`, compare la version et la révision source, puis demande confirmation avant tout remplacement.

```sh
wol update --check
```

La vérification ne modifie rien et ne demande pas de privilèges administrateur.

## Canal de développement

```sh
sudo wol update --channel dev
```

Un avertissement BETA/DEV est affiché. Ce choix vaut uniquement pour cette commande : l'exécution suivante de `sudo wol update` revient automatiquement à `main`.

L'installateur choisit l'architecture et télécharge le manifeste, sa signature et le binaire depuis le même commit de publication. Il n'existe aucun fallback de compilation côté client. Une erreur réseau, une branche absente ou un binaire indisponible est signalé sans modifier l'installation.

La configuration, les jetons, secrets, données, politiques UPS, réseaux et relais ne sont pas modifiés. L'état activé/désactivé du service est conservé. Une tâche interrompue reste soumise à la revalidation du moteur existant.

## Installation atomique et rollback

Avant remplacement, l'installateur valide le nouveau binaire et la configuration, puis sauvegarde le binaire, le service, l'installateur et les métadonnées sous `/var/lib/wol-manager/backups/`. Le remplacement du binaire utilise un fichier temporaire sur le même système de fichiers.

Si le lancement, la validation YAML ou le redémarrage systemd échoue, ces composants sont restaurés et l'ancien service est relancé lorsque possible. Aucune migration irréversible de données n'est exécutée dans cette Beta ; les données persistantes restent intactes. Le journal indique clairement si le rollback a été déclenché.

## Intégrité et provenance

La clé publique Ed25519 approuvée est installée avec WoL Manager. Elle vérifie le manifeste avant lecture de ses champs ; le SHA-256 signé vérifie ensuite le binaire. La version, le canal et la révision source intégrés doivent correspondre au manifeste. La branche GitHub est d'abord résolue vers un commit de publication, afin de ne jamais mélanger des fichiers de commits différents.

Le `source_revision` désigne le commit source propre utilisé pour compiler. Le commit de publication, nécessairement ultérieur puisqu'il contient les binaires, est enregistré séparément dans `release.conf`. Cette séparation évite le problème circulaire d'un binaire contenant le hash du commit qui le contient.
