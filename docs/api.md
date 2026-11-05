# API REST

L'API suit le contrat [`api/openapi.yaml`](../api/openapi.yaml). Les handlers
Go restent la référence d'exécution. Toutes les routes, **y compris
`/health`**, exigent `Authorization: Bearer <jeton>`.

## Démarrage et authentification

Le service écoute par défaut en HTTP sur `127.0.0.1:8080`. HTTP sans TLS est
autorisé uniquement sur loopback ; une écoute non locale exige un certificat
et une clé TLS valides.

```sh
export WOL_API='http://127.0.0.1:8080/api/v1'
read -r -s -p 'Jeton API : ' WOL_TOKEN; echo
curl --fail-with-body -H "Authorization: Bearer $WOL_TOKEN" "$WOL_API/health"
```

Créez le jeton avec `sudo wol token create --help`. Le magasin est relu à
chaque authentification : une création ou révocation s'applique dès la requête
suivante. `read` autorise les consultations, `wake` les réveils et `admin` les
opérations administratives ; `admin` implique les autres permissions.

Chaque client est limité par le couple identifiant du jeton/adresse IP selon
`api.rate_per_minute`. Les réponses utilisent `application/json`,
`Cache-Control: no-store` et `X-Content-Type-Options: nosniff`.

## Règles communes

- Aucun paramètre de requête n'est actuellement défini.
- Les corps JSON sont limités à 64 Kio, refusent les champs inconnus et ne
  peuvent contenir qu'un seul objet.
- Les opérations réseau doivent d'abord écrire l'événement `requested` dans le
  journal durable. En cas d'échec, l'API renvoie `503 audit_unavailable` et ne
  crée pas de tâche.
- Deux tâches actives incompatibles peuvent produire `409 job_conflict`.
- Les erreurs ont la forme
  `{"error":{"code":"not_found","message":"serveur inconnu"}}`.
- Toutes les routes peuvent renvoyer `401 unauthorized`, `429 rate_limited`
  et `405 method_not_allowed`. Une permission insuffisante renvoie
  `403 forbidden`.

L'API ne permet pas de fournir librement une MAC, une IP ou une destination
WoL. Elle résout uniquement des identifiants présents dans la configuration.

## Tâches et niveau de preuve

Les réveils et découvertes répondent `202 Accepted` avec une tâche. Cela
signifie seulement que la demande asynchrone a été acceptée. Il faut consulter
la tâche jusqu'à un état terminal : `completed`, `failed` ou `cancelled`.
`queued`, `running` et `revalidation_required` ne sont pas terminaux.

Même une tâche `completed` doit être interprétée précisément :

- `socket_accepted: true` : le noyau a accepté l'envoi local ;
- `packet_emitted: "unverified"` : aucune capture indépendante ne confirme le
  paquet ;
- `wake_verified: true` : les contrôles configurés ont observé la machine.

Ces trois niveaux ne prouvent pas à eux seuls un démarrage physique. Les
tâches terminales sont conservées au plus 30 jours et dans la limite des 1 000
plus récentes ; les tâches actives ou en revalidation ne sont pas évincées.

## Endpoints

Dans les exemples, définissez d'abord :

```sh
AUTH=(-H "Authorization: Bearer $WOL_TOKEN")
```

### `GET /health`

- Permission : `read`.
- Paramètres/corps : aucun.
- Réponse `200` représentative : `{"status":"ok","version":"1.2.3"}`.

```sh
curl --fail-with-body "${AUTH[@]}" "$WOL_API/health"
```

### `GET /servers`

- Permission : `read`.
- Paramètres/corps : aucun.
- Réponse `200` : objet indexé par identifiant, contenant les champs publics
  des machines.

```sh
curl --fail-with-body "${AUTH[@]}" "$WOL_API/servers"
```

```json
{"nas":{"name":"NAS","mac":"02:00:00:00:00:01","ip":"192.168.1.20","network":"lan","groups":["infra"],"dependencies":[],"checks":[],"transports":["local_broadcast"],"enabled":true}}
```

### `GET /servers/{id}/status`

- Permission : `read`.
- Chemin : `id`, identifiant d'une machine configurée.
- Réponses : `200` avec l'état et les contrôles ; `404 not_found` si la
  machine est inconnue.

```sh
curl --fail-with-body "${AUTH[@]}" "$WOL_API/servers/nas/status"
```

La réponse distingue notamment `reachable`, `unreachable` et `unknown`. Une
machine injoignable n'est pas nécessairement éteinte.

### `POST /servers/{id}/wake`

- Permission : `wake`.
- Chemin : `id`, identifiant d'une machine configurée.
- Corps facultatif : `{"dry_run":true}` ; sans corps, le réveil est réel.
- Réponses : `202` avec la tâche ; `400 invalid_json`, `404 not_found`,
  `409 job_conflict` ou `503 audit_unavailable`.

```sh
curl --fail-with-body -X POST "${AUTH[@]}" \
  -H 'Content-Type: application/json' --data '{"dry_run":true}' \
  "$WOL_API/servers/nas/wake"
```

```json
{"id":"job-123","kind":"wake","target":"nas","state":"queued","created":"2026-11-04T10:00:00Z","updated":"2026-11-04T10:00:00Z","actor":"api-client"}
```

### `POST /groups/{id}/wake`

- Permission : `wake`.
- Chemin : `id`, identifiant d'un groupe configuré.
- Corps : aucun.
- Réponses : `202` avec une tâche `wake_group`, `404 not_found`,
  `409 job_conflict` ou `503 audit_unavailable`.

```sh
curl --fail-with-body -X POST "${AUTH[@]}" "$WOL_API/groups/infra/wake"
```

### `POST /wake-all`

- Permission : `wake`.
- Paramètres/corps : aucun.
- Réponses : `202` avec une tâche `wake_all`, `409 job_conflict` ou
  `503 audit_unavailable`.

```sh
curl --fail-with-body -X POST "${AUTH[@]}" "$WOL_API/wake-all"
```

### `GET /ups`

- Permission : `read`.
- Paramètres/corps : aucun.
- Réponse `200` : objet indexé par UPS avec `state`, statut NUT brut,
  batterie/autonomie lorsqu'elles sont connues et éventuelle erreur.

```sh
curl --fail-with-body "${AUTH[@]}" "$WOL_API/ups"
```

```json
{"ups-main":{"id":"ups-main","state":"OL","raw_status":"OL","battery_percent":100,"battery_known":true,"runtime_known":false}}
```

Une mesure absente n'est jamais interprétée comme zéro.

### `GET /automations`

- Permission : `read`.
- Paramètres/corps : aucun.
- Réponse `200` : `cycles`, `policies` et `jobs`. Si le moteur automatique
  n'est pas attaché, `cycles` et `policies` sont des listes vides.

```sh
curl --fail-with-body "${AUTH[@]}" "$WOL_API/automations"
```

Les durées internes des politiques sont exposées en nanosecondes dans les
champs portant le suffixe `_ns`.

### `GET /jobs/{id}`

- Permission : `read`.
- Chemin : identifiant de tâche renvoyé par une opération asynchrone.
- Réponses : `200` avec la tâche et son éventuel `result` ; `404 not_found`.

```sh
curl --fail-with-body "${AUTH[@]}" "$WOL_API/jobs/job-123"
```

### `POST /jobs/{id}/cancel`

- Permission : `admin`.
- Chemin : identifiant de tâche.
- Corps : aucun.
- Réponse `202` : `{"status":"cancellation_requested"}`.
- Réponse `409 not_cancellable` : tâche inconnue, déjà terminale ou non
  annulable. Si la fin naturelle gagne la course, son état reste terminal.

```sh
curl --fail-with-body -X POST "${AUTH[@]}" \
  "$WOL_API/jobs/job-123/cancel"
```

### `GET /networks`

- Permission : `read`.
- Paramètres/corps : aucun.
- Réponse `200` : objet indexé par identifiant avec CIDR, interface, adresse
  source, autorisations, relais, transports, contrôles et port.

```sh
curl --fail-with-body "${AUTH[@]}" "$WOL_API/networks"
```

### `GET /networks/diagnostics`

- Permission : `read`.
- Paramètres/corps : aucun.
- Réponse `200` : pour chaque machine, un `plan` de transport et une chaîne
  `error` vide ou explicative.

```sh
curl --fail-with-body "${AUTH[@]}" "$WOL_API/networks/diagnostics"
```

Le plan n'émet aucun Magic Packet et ne prouve pas le réveil.

### `POST /networks/discovery`

- Permission : `admin`.
- Corps obligatoire : `active`, `confirm` et `dry_run`, booléens.
- Réponses : `202` avec une tâche `network_discovery`, `400 invalid_json`,
  `403 passive_discovery_blocked` ou `active_discovery_blocked`,
  `409 job_conflict`, `503 audit_unavailable`.

Découverte passive :

```sh
curl --fail-with-body -X POST "${AUTH[@]}" \
  -H 'Content-Type: application/json' \
  --data '{"active":false,"confirm":false,"dry_run":true}' \
  "$WOL_API/networks/discovery"
```

Diagnostic actif simulé :

```sh
curl --fail-with-body -X POST "${AUTH[@]}" \
  -H 'Content-Type: application/json' \
  --data '{"active":true,"confirm":true,"dry_run":true}' \
  "$WOL_API/networks/discovery"
```

Le mode actif exige à la fois l'activation globale, l'autorisation API et
`confirm: true`. `dry_run: false` peut produire du trafic réseau borné par les
CIDR, budgets, débits et délais configurés.

## Limites et précautions

L'API ne propose aucun CRUD de configuration. Utilisez les assistants CLI et
redémarrez le processus permanent si sa configuration a changé. Ne placez
jamais un jeton réel dans un script versionné ou une URL. Pour une écoute
distante, utilisez HTTPS et protégez la clé privée du serveur.
