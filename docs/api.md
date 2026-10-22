# API REST

L'API suit le contrat [`api/openapi.yaml`](../api/openapi.yaml). Toutes les routes exigent `Authorization: Bearer <jeton>`, y compris `/health`.

Le magasin de jetons est relu atomiquement à chaque authentification. Un jeton créé est donc accepté
dès la requête suivante et un jeton révoqué est immédiatement refusé, sans redémarrage du service.
Les requêtes déjà authentifiées terminent normalement. Aucun secret ou hash n'est journalisé ni
affiché par `wol token list`.

Permissions : `read` pour inventaire, état et jobs ; `wake` pour les réveils ; `admin` pour annulation et découverte active. `admin` implique les autres permissions.

Les opérations longues renvoient `202` et un job. Consulter `GET /api/v1/jobs/{id}` ou demander son annulation. `GET /api/v1/automations` expose en lecture seule les cycles électriques persistés, politiques, tâches et raisons d'attente. Une découverte active requiert simultanément : activation globale, activation API, permission `admin`, `active: true` et `confirm: true`.

L'API n'accepte jamais d'adresse IP, de MAC ou de destination WoL libre. Le client fournit uniquement un identifiant configuré.

HTTP sans TLS est accepté uniquement sur loopback. Toute autre écoute échoue à la validation sans certificat et clé TLS.

## Routes et permissions

| Méthode et route | Permission | Fonction |
|---|---|---|
| `GET /api/v1/health` | `read` | santé du service |
| `GET /api/v1/servers` | `read` | inventaire des machines |
| `GET /api/v1/servers/{id}/status` | `read` | contrôles d'une machine |
| `POST /api/v1/servers/{id}/wake` | `wake` | crée un job de réveil |
| `POST /api/v1/groups/{id}/wake` | `wake` | crée un job de groupe |
| `POST /api/v1/wake-all` | `wake` | crée un job global |
| `GET /api/v1/ups` | `read` | états NUT en lecture seule |
| `GET /api/v1/automations` | `read` | cycles, politiques, progressions et attentes |
| `GET /api/v1/jobs/{id}` | `read` | consulte un job |
| `POST /api/v1/jobs/{id}/cancel` | `admin` | demande une annulation |
| `GET /api/v1/networks` | `read` | réseaux configurés |
| `GET /api/v1/networks/diagnostics` | `read` | plans de transport |
| `POST /api/v1/networks/discovery` | `admin` | crée un job de découverte |

Les endpoints de réveil et de découverte répondent `202` avec l'identifiant du job. Un job terminé
reste dans un état terminal `completed`, `failed` ou `cancelled`. Une annulation qui arrive après la
fin naturelle retourne `409`; elle ne remplace jamais rétroactivement le résultat terminé.

L'API ne fournit pas de CRUD pour modifier machines, réseaux, groupes ou politiques. Utilisez les
assistants CLI, puis redémarrez le service si la configuration du processus permanent a changé.
