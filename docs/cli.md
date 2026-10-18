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

Codes de sortie : `0` succès, `1` erreur d'exécution/configuration/politique, `2` syntaxe invalide. Les diagnostics actifs affichent toujours un avertissement sur stderr.

Sur un terminal compatible, la CLI met en évidence les erreurs en rouge, les avertissements en jaune, les validations réussies en vert et les informations importantes en cyan. Les couleurs sont automatiquement désactivées pour une redirection, une sortie JSON, un terminal `dumb` ou lorsque la variable standard `NO_COLOR` est définie. Les textes et codes de sortie restent identiques pour les scripts.

L'installateur place automatiquement l'autocomplétion Bash au bon emplacement. Dans un nouveau terminal, `wol m<Tab>` complète `menu` et `wol config m<Tab>` complète `machine`. La commande `wol completion bash` permet aussi d'afficher le script sans l'installer.

`wol --help` affiche l'aide générale sans charger la configuration. Les assistants disposent aussi d'une aide dédiée, par exemple `wol config network --help` ou `wol config machine --help`. Ces aides ne nécessitent ni privilège, ni fichier YAML, ni accès réseau.
