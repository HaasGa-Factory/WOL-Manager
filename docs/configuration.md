# Configuration

La méthode recommandée est `sudo wol setup`. L'assistant détecte les interfaces IPv4 et permet de gérer les réseaux, machines, groupes, dépendances et UPS sans ouvrir `/etc/wol-manager/wol.yaml`. Les commandes `sudo wol config network`, `machine`, `group` et `nut` ouvrent directement la section correspondante.

Avant chaque enregistrement, la configuration complète est validée. Une configuration existante reçoit une sauvegarde datée, puis le nouveau fichier est remplacé atomiquement. Les cibles symboliques sont refusées et, sous Debian avec une installation standard, le fichier final est `root:wol-manager` en mode `0640`. Une suppression référencée est refusée au lieu de provoquer une cascade silencieuse.

Le YAML est lu avec rejet des champs inconnus. `wol config check` vérifie notamment les MAC/IP dupliquées, CIDR, références, priorités de groupe, cycles, TLS, réseaux actifs autorisés et transports.

Ordre de surcharge : valeurs internes → `global` → réseau → groupes par `priority` croissante → serveur → option CLI sûre. Une option CLI ne peut pas lever une restriction réseau ou UPS.

## Vérification après réveil

L'assistant `sudo wol setup` permet de régler les délais globaux et les surcharges propres à chaque machine. Les commandes guidées évitent toute édition manuelle du YAML.

```yaml
global:
  verification:
    initial_delay: 0s
    timeout: 45s
    interval: 2s

servers:
  truenas:
    verification:
      initial_delay: 90s
      timeout: 180s
      interval: 5s
```

`initial_delay` attend après l'envoi effectif du paquet, avant la première vérification. `timeout` limite la fenêtre totale de vérification et `interval` espace les tentatives. Les durées utilisent la syntaxe Go (`5s`, `90s`, `3m`). Une machine déjà joignable, une simulation `--dry-run` ou un envoi échoué n'attendent jamais ce délai. L'attente est liée au contexte du job et s'interrompt immédiatement lors d'une annulation.

Sans ces champs, le comportement historique reste inchangé : aucune attente initiale, fenêtre de 45 secondes et intervalle de 2 secondes. `wake_timeout` reste accepté pour les anciennes configurations. Une surcharge ponctuelle ne modifie pas le fichier :

```sh
wol wake truenas --verification-delay 90s
```

Deux groupes ne peuvent pas partager la même priorité. Cette règle rend les surcharges déterministes. Les listes `servers` d'un groupe et `groups` d'un serveur sont réunies sans doublon.

Les secrets ne figurent pas dans le YAML partagé : les jetons sont dans `api.tokens_file`, les identifiants NUT et clés TLS sont référencés par chemin. Utiliser des fichiers `0600`, détenus par l'utilisateur du service.

Le YAML reste documenté pour les administrateurs avancés dans [`configs/wol.example.yaml`](../configs/wol.example.yaml), mais son édition manuelle n'est pas nécessaire. La découverte active est désactivée par défaut. Les CIDR publics sont refusés dans `allowed_subnets`.
