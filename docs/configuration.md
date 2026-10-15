# Configuration

Le YAML est lu avec rejet des champs inconnus. `wol config check` vérifie notamment les MAC/IP dupliquées, CIDR, références, priorités de groupe, cycles, TLS, réseaux actifs autorisés et transports.

Ordre de surcharge : valeurs internes → `global` → réseau → groupes par `priority` croissante → serveur → option CLI sûre. Une option CLI ne peut pas lever une restriction réseau ou UPS.

Deux groupes ne peuvent pas partager la même priorité. Cette règle rend les surcharges déterministes. Les listes `servers` d'un groupe et `groups` d'un serveur sont réunies sans doublon.

Les secrets ne figurent pas dans le YAML partagé : les jetons sont dans `api.tokens_file`, les identifiants NUT et clés TLS sont référencés par chemin. Utiliser des fichiers `0600`, détenus par l'utilisateur du service.

Voir [`configs/wol.example.yaml`](../configs/wol.example.yaml) pour le schéma complet. La découverte active est désactivée par défaut. Les CIDR publics sont refusés dans `allowed_subnets`.
