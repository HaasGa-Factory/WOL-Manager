# Configuration

La méthode recommandée est `sudo wol setup`. L'assistant détecte les interfaces IPv4 et permet de gérer les réseaux, machines, groupes, dépendances, découverte et UPS sans ouvrir `/etc/wol-manager/wol.yaml`. Les commandes `sudo wol config network`, `machine`, `group`, `nut` et `discovery` ouvrent directement la section correspondante. Dans chaque champ, `?` affiche l'aide sans avancer ni modifier le défaut ; une saisie invalide est redemandée.

Chaque section accepte `--help` sans charger le YAML. Dans un assistant ouvert, saisissez `?` pour
afficher une explication du menu courant sans modifier les données préparées.

Avant chaque enregistrement, la configuration complète est validée. Une configuration existante reçoit une sauvegarde datée, puis le nouveau fichier est remplacé atomiquement. Les cibles symboliques sont refusées et, sous Debian avec une installation standard, le fichier final est `root:wol-manager` en mode `0640`. Une suppression référencée est refusée au lieu de provoquer une cascade silencieuse.

`wol config discovery` enregistre ensemble les deux parties de la fonction : `network_discovery`
(passif, actif, CIDR, budget, débit et timeout) et `api.allow_active_discovery`. Après confirmation,
la commande affiche explicitement que la configuration a été enregistrée. Une annulation ne remplace
pas le fichier. Le résumé final présente toutes ces valeurs avant l'écriture.

Le YAML est lu avec rejet des champs inconnus. `wol config check` vérifie notamment les MAC/IP dupliquées, CIDR, références, priorités de groupe, cycles, TLS, réseaux actifs autorisés et transports.

L'assistant couvre les paramètres usuels. Les réglages avancés de réseau et de machine — relais,
ordre détaillé des transports, politiques UPS historiques, CA NUT privée et fichiers d'identifiants —
restent disponibles dans le YAML. Cette limite est annoncée dans `wol config ... --help`; elle ne
change pas la compatibilité des configurations existantes.

Ordre de surcharge : valeurs internes → `global` → réseau → groupes par `priority` croissante → serveur → option CLI sûre. Une option CLI ne peut pas lever une restriction réseau ou UPS.

## Vérification après réveil

L'assistant `sudo wol setup` permet de régler les délais globaux et les surcharges propres à chaque machine. Les commandes guidées évitent toute édition manuelle du YAML.

```yaml
global:
  check_logic: any
  verification:
    initial_delay: 0s
    timeout: 45s
    interval: 2s
    attempts: 0
    packet_retries: 1

servers:
  truenas:
    check_logic: all
    verification:
      initial_delay: 90s
      timeout: 180s
      interval: 5s
```

`initial_delay` attend après l'envoi effectif du paquet, avant la première vérification. `timeout` limite la fenêtre totale de vérification et `interval` espace les tentatives. `attempts: 0` conserve une limite basée uniquement sur `timeout`; une valeur positive borne aussi le nombre de contrôles. `packet_retries` borne séparément les nouveaux Magic Packets. Les durées utilisent la syntaxe Go (`5s`, `90s`, `3m`). Une machine déjà joignable, une simulation `--dry-run` ou un envoi échoué n'attendent jamais ce délai. L'attente est liée au contexte du job et s'interrompt immédiatement lors d'une annulation.

Chaque machine peut déclarer plusieurs contrôles, y compris plusieurs ports TCP. `check_logic: any` considère la machine joignable dès qu'un contrôle réussit ; `all` exige la réussite de tous les contrôles. Un résultat absent ou indéterminé n'est jamais transformé en succès.

Sans ces champs, le comportement historique reste inchangé : aucune attente initiale, fenêtre de 45 secondes et intervalle de 2 secondes. `wake_timeout` reste accepté pour les anciennes configurations. Une surcharge ponctuelle ne modifie pas le fichier :

```sh
wol wake truenas --verification-delay 90s
```

Deux groupes ne peuvent pas partager la même priorité. Cette règle rend les surcharges déterministes. Les listes `servers` d'un groupe et `groups` d'un serveur sont réunies sans doublon.

## Reprise automatique après coupure

La politique se configure dans l'assistant de groupe. Exemple administrateur équivalent :

```yaml
groups:
  infrastructure:
    priority: 10
    servers: [truenas, proxmox]
    power_restore:
      enabled: true
      ups: ups-main
      low_battery: 40
      high_battery: 70
      stable_for: 30s
      poll_interval: 5s
      mode: sequential
      order: [truenas, proxmox]
      failure_policy: stop
      retry_limit: 2
      retry_interval: 15s
```

Le seuil bas réarme un groupe pendant le cycle observé ; le seuil haut autorise ensuite son démarrage. La batterie doit être connue et le secteur `OL` stable. Les politiques d'échec sont `stop`, `continue`, `retry` et `continue_retry`. Les dépendances obligatoires restent prioritaires dans tous les modes.

La surveillance est exécutée par le processus permanent `wol api serve`, normalement lancé par
systemd. `wol automation status` consulte les cycles et raisons d'attente persistés mais ne démarre
pas la surveillance.

Les secrets ne figurent pas dans le YAML partagé : les jetons sont dans `api.tokens_file`, les identifiants NUT et clés TLS sont référencés par chemin. Utiliser des fichiers `0600`, détenus par l'utilisateur du service.

Les aides `?` sont propres au champ : identifiants de machine, groupe, réseau, serveur NUT et UPS ne
sont pas confondus. Les dépendances indiquent quelle machine doit réussir en premier. Les cinq notions
de vérification restent séparées : attente initiale, timeout total, intervalle, nombre de contrôles et
nouveaux Magic Packets. Une durée sans unité telle que `30` est refusée avec la suggestion `30s`, sans
perdre l'objet en cours.

Le YAML reste documenté pour les administrateurs avancés dans [`configs/wol.example.yaml`](../configs/wol.example.yaml), mais son édition manuelle n'est pas nécessaire. La découverte active est désactivée par défaut. Les CIDR publics sont refusés dans `allowed_subnets`.
