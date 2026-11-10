# Sauvegarde et restauration

WoL Manager 1.3.0 fournit un format portable `.wolbackup` pour sauvegarder et
restaurer les données administratives sans inclure le programme, les journaux
ou les caches.

## Choisir le bon niveau

| Niveau | Commande | Contenu | Usage conseillé |
|---|---|---|---|
| Configuration | `sudo wol backup create` | `wol.yaml` validé | **Défaut recommandé** avant une modification ou une mise à jour |
| Configuration et jetons | `sudo wol backup create --with-tokens` | configuration et hashes des jetons API | migration conservant les accès API |
| Complète | `sudo wol backup create --full` | configuration, jetons, jobs et cycles | reprise administrative complète avec précautions supplémentaires |

Les secrets bruts des jetons ne peuvent pas être sauvegardés : WoL Manager ne
stocke que leurs hashes. Les fichiers référencés par la configuration, comme
les mots de passe NUT ou les clés privées d'un relais, ne sont jamais copiés
automatiquement.

## Créer une sauvegarde

Par défaut, l'archive est écrite sous `/var/backups/wol-manager` :

```sh
sudo wol backup create
```

Pour choisir un fichier ou un répertoire :

```sh
sudo wol backup create --output /media/usb/wol-manager/
sudo wol backup create --full --output /media/usb/wol-manager-complet.wolbackup
```

Le répertoire système est créé en `0700` et les archives en `0600`. Un fichier
existant n'est jamais écrasé. Les liens symboliques sont refusés.

## Lister, inspecter et vérifier

```sh
sudo wol backup list
sudo wol backup inspect /var/backups/wol-manager/SAUVEGARDE.wolbackup
sudo wol backup verify /var/backups/wol-manager/SAUVEGARDE.wolbackup
```

`inspect` présente la date, la version, les sections et le nombre d'objets sans
afficher le YAML, les hashes de jetons ni les données des jobs. `verify`
contrôle le format, les chemins internes, les limites, les SHA-256 et la
validité des données embarquées.

> Les SHA-256 détectent une corruption accidentelle. L'archive n'est ni signée
> ni chiffrée : ils ne prouvent pas son origine et ne protègent pas son contenu
> contre une personne pouvant lire le fichier.

## Simuler une restauration

La configuration est la seule section restaurée par défaut :

```sh
sudo wol backup restore SAUVEGARDE.wolbackup --dry-run
```

Pour simuler plusieurs sections :

```sh
sudo wol backup restore SAUVEGARDE.wolbackup \
  --sections config,tokens,state --dry-run
```

La simulation vérifie l'archive et décrit les différences sans modifier de
fichier.

## Restaurer

```sh
sudo wol backup restore SAUVEGARDE.wolbackup
```

Avant toute écriture, WoL Manager crée une sauvegarde de sécurité de l'état
actuel. Chaque fichier est remplacé atomiquement, puis la configuration est
rechargée et validée. Si une étape échoue, les fichiers déjà remplacés sont
restaurés.

Une configuration absente peut être recréée depuis l'archive. Une configuration
présente mais invalide n'est pas écrasée automatiquement : corrigez-la ou
mettez-la de côté après en avoir conservé une copie indépendante.

### Restaurer les jetons

```sh
sudo wol backup restore SAUVEGARDE.wolbackup --sections config,tokens
```

Les permissions `0600` sont réappliquées. Les secrets originaux ne sont pas
révélés ; seuls les hashes présents dans l'archive sont restaurés.

### Restaurer les jobs et cycles

Arrêtez d'abord le service :

```sh
sudo systemctl stop wol-manager.service
sudo wol backup restore SAUVEGARDE.wolbackup --sections all
sudo systemctl start wol-manager.service
```

La restauration est refusée si le service est actif. Les jobs sauvegardés dans
les états `queued` ou `running` deviennent `revalidation_required` : aucun
Magic Packet n'est envoyé automatiquement à la suite de la restauration.

## Nettoyer les anciennes sauvegardes

Conserver les dix plus récentes :

```sh
sudo wol backup prune --dry-run
sudo wol backup prune
```

Choisir une autre rétention :

```sh
sudo wol backup prune --keep 20
```

Seules les archives valides et régulières du répertoire demandé sont prises en
compte. Les fichiers inconnus, invalides et liens symboliques sont ignorés. La
dernière sauvegarde valide n'est jamais supprimée.

## Copie hors machine

Une sauvegarde locale ne protège pas contre la perte du disque. Copiez au moins
une archive vérifiée vers un support ou un coffre séparé, puis protégez ce
support comme la configuration elle-même. Après la copie :

```sh
chmod 600 SAUVEGARDE.wolbackup
sudo wol backup verify SAUVEGARDE.wolbackup
```

Conservez séparément les fichiers secrets externes référencés par le YAML. Ne
placez jamais une archive `.wolbackup` dans Git.
