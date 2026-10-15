# API REST

L'API suit le contrat [`api/openapi.yaml`](../api/openapi.yaml). Toutes les routes exigent `Authorization: Bearer <jeton>`, y compris `/health`.

Permissions : `read` pour inventaire, état et jobs ; `wake` pour les réveils ; `admin` pour annulation et découverte active. `admin` implique les autres permissions.

Les opérations longues renvoient `202` et un job. Consulter `GET /api/v1/jobs/{id}` ou demander son annulation. Une découverte active requiert simultanément : activation globale, activation API, permission `admin`, `active: true` et `confirm: true`.

L'API n'accepte jamais d'adresse IP, de MAC ou de destination WoL libre. Le client fournit uniquement un identifiant configuré.

HTTP sans TLS est accepté uniquement sur loopback. Toute autre écoute échoue à la validation sans certificat et clé TLS.
