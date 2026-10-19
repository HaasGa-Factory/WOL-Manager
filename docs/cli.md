# CLI

Syntaxe générale : `wol [--config FILE] [--json] COMMANDE`.

| Commande | Fonction |
|---|---|
| `wol setup` | assistant complet : réseaux, machines, groupes, dépendances et NUT |
| `wol update --check` | vérifie `main` sans privilèges ni modification |
| `wol update` | met à jour depuis les binaires signés de `main` |
| `wol update --check --channel dev` | vérifie temporairement le canal `dev` |
| `wol update --channel dev` | installe temporairement le canal `dev` |
| `wol version` | affiche la version installée |
| `wol list` | liste les serveurs |
| `wol status ID` | exécute les vérifications configurées |
| `wol wake ID [--dry-run] [--verification-delay 90s]` | dépendances puis serveur ; surcharge temporaire du délai avant vérification |
| `wol wake --group ID` | groupe |
| `wol wake --all` | tous les serveurs actifs |
| `wol config check` | validation stricte |
| `wol config network` | ajoute, modifie ou supprime un réseau avec validation |
| `wol config machine` | ajoute, modifie ou supprime une machine avec validation |
| `wol config group` | ajoute, modifie ou supprime un groupe avec validation |
| `wol config nut` | configure et teste les serveurs NUT et leurs UPS |
| `wol config edit` | édition YAML avancée, non requise pour un usage normal |
| `wol token create` | ajoute un hash et affiche le jeton une fois |
| `wol network test [ID]` | explique les transports |
| `wol network discover` | découverte passive |
| `wol network discover --active --allow-active-scan` | diagnostic actif |
| `wol ups status` | états NUT |
| `wol ups test` | diagnostic en lecture seule de tous les UPS configurés |
| `wol jobs` | état persistant des tâches |
| `wol automation status` | cycles NUT, politiques, progressions et raisons d'attente |
| `wol menu` | consultation, configuration, simulation et réveil confirmé |
| `wol api serve` | API et moteur de surveillance/reprise NUT |
| `wol relay serve` | relais HTTPS mTLS |
| `wol relay check ID` | disponibilité d'un relais |
| `wol completion bash` | génère le script d'autocomplétion Bash |

`wol COMMANDE --help` affiche l'aide détaillée avant de charger la configuration. Cette aide ne
nécessite donc ni privilèges, ni fichier YAML, ni accès au réseau. Les commandes composées acceptent
la même forme, par exemple :

```sh
wol wake --help
wol config group --help
wol network discover --help
wol automation status --help
wol relay serve --help
```

Dans `wol setup`, `wol config network|machine|group|nut` et `wol menu`, saisir `?` affiche l'aide
du menu courant sans modifier la configuration.

## Options importantes

| Commande | Options |
|---|---|
| `wol update` | `--check`, `--channel main|dev` |
| `wol wake` | `--dry-run`, `--verification-delay DURÉE`, `--group ID`, `--all` |
| `wol token create` | `--id ID`, `--permissions read,wake,admin` |
| `wol network discover` | `--active`, `--allow-active-scan`, `--dry-run` |
| `wol relay serve` | `--id`, `--listen`, `--cert`, `--key`, `--client-ca` |
| `wol version` | `--channel`, `--source-revision` ou `--commit` |

`wol network test` accepte zéro, un ou plusieurs identifiants. Sans identifiant, il explique le
plan de transport de toutes les machines. Il n'envoie aucun paquet.

`wol jobs` et `wol automation status` produisent toujours du JSON afin de conserver les états et
raisons d'attente sans perte d'information. Les états de tâche sont `queued`, `running`,
`revalidation_required`, `completed`, `failed` et `cancelled`.

Le moteur permanent de surveillance et de reprise NUT s'exécute avec `wol api serve`, normalement
lancé par le service systemd. `wol automation status` consulte son état persisté mais ne démarre pas
le moniteur à lui seul.

Codes de sortie : `0` succès, `1` erreur d'exécution/configuration/politique, `2` syntaxe invalide. Les diagnostics actifs affichent toujours un avertissement sur stderr.

Sur un terminal compatible, la CLI met en évidence les erreurs en rouge, les avertissements en jaune, les validations réussies en vert et les informations importantes en cyan. Les couleurs sont automatiquement désactivées pour une redirection, une sortie JSON, un terminal `dumb` ou lorsque la variable standard `NO_COLOR` est définie. Les textes et codes de sortie restent identiques pour les scripts.

L'installateur place automatiquement l'autocomplétion Bash au bon emplacement. Dans un nouveau terminal, `wol m<Tab>` complète `menu` et `wol config m<Tab>` complète `machine`. Les options d'aide et de version sont également proposées. La commande `wol completion bash` permet aussi d'afficher le script sans l'installer.

`wol --help` affiche l'aide générale sans charger la configuration. Chaque commande et sous-commande documentée ci-dessus possède également une aide contextuelle.
