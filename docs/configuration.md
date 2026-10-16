# Configuration

La méthode recommandée est `sudo wol setup`. L'assistant détecte les interfaces IPv4 et permet de gérer les réseaux, machines, groupes, dépendances et UPS sans ouvrir `/etc/wol-manager/wol.yaml`. Les commandes `sudo wol config network`, `machine`, `group` et `nut` ouvrent directement la section correspondante.

Avant chaque enregistrement, la configuration complète est validée. Une configuration existante reçoit une sauvegarde datée, puis le nouveau fichier est remplacé atomiquement. Les cibles symboliques sont refusées et, sous Debian avec une installation standard, le fichier final est `root:wol-manager` en mode `0640`. Une suppression référencée est refusée au lieu de provoquer une cascade silencieuse.

Le YAML est lu avec rejet des champs inconnus. `wol config check` vérifie notamment les MAC/IP dupliquées, CIDR, références, priorités de groupe, cycles, TLS, réseaux actifs autorisés et transports.

Ordre de surcharge : valeurs internes → `global` → réseau → groupes par `priority` croissante → serveur → option CLI sûre. Une option CLI ne peut pas lever une restriction réseau ou UPS.

Deux groupes ne peuvent pas partager la même priorité. Cette règle rend les surcharges déterministes. Les listes `servers` d'un groupe et `groups` d'un serveur sont réunies sans doublon.

Les secrets ne figurent pas dans le YAML partagé : les jetons sont dans `api.tokens_file`, les identifiants NUT et clés TLS sont référencés par chemin. Utiliser des fichiers `0600`, détenus par l'utilisateur du service.

Le YAML reste documenté pour les administrateurs avancés dans [`configs/wol.example.yaml`](../configs/wol.example.yaml), mais son édition manuelle n'est pas nécessaire. La découverte active est désactivée par défaut. Les CIDR publics sont refusés dans `allowed_subnets`.
