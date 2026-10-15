# CLI

Syntaxe générale : `wol [--config FILE] [--json] COMMANDE`.

| Commande | Fonction |
|---|---|
| `wol setup` | crée ou adapte prudemment la configuration, dont NUT |
| `wol update --check` | vérifie `main` sans privilèges ni modification |
| `wol update` | met à jour depuis les binaires signés de `main` |
| `wol update --check --channel dev` | vérifie temporairement le canal `dev` |
| `wol update --channel dev` | installe temporairement le canal `dev` |
| `wol version` | affiche la version installée |
| `wol list` | liste les serveurs |
| `wol status ID` | exécute les vérifications configurées |
| `wol wake ID [--dry-run]` | dépendances puis serveur |
| `wol wake --group ID` | groupe |
| `wol wake --all` | tous les serveurs actifs |
| `wol config check` | validation stricte |
| `wol config edit` | ouvre `$EDITOR`, `vi` par défaut |
| `wol token create` | ajoute un hash et affiche le jeton une fois |
| `wol network test [ID]` | explique les transports |
| `wol network discover` | découverte passive |
| `wol network discover --active --allow-active-scan` | diagnostic actif |
| `wol ups status` | états NUT |
| `wol ups test` | diagnostic en lecture seule de tous les UPS configurés |
| `wol jobs` | état persistant des tâches |
| `wol menu` | interface interactive |
| `wol api serve` | API et automatisations |
| `wol relay serve` | relais HTTPS mTLS |
| `wol relay check ID` | disponibilité d'un relais |

Codes de sortie : `0` succès, `1` erreur d'exécution/configuration/politique, `2` syntaxe invalide. Les diagnostics actifs affichent toujours un avertissement sur stderr.
