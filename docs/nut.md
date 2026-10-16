# Intégration NUT

NUT est facultatif. Sans `ups` ni politique UPS, WoL Manager n'ouvre aucune connexion NUT.

Dans `sudo wol setup`, choisissez la section NUT, ou ouvrez-la directement avec `sudo wol config nut`. L'assistant propose de tester `127.0.0.1:3493`, exécute `LIST UPS`, permet une sélection multiple et exige une lecture valide de `ups.status` avant enregistrement. Un serveur distant peut être ajouté même si le serveur local fonctionne. L'assistant ne scanne jamais le réseau, n'installe pas NUT, ne modifie pas sa configuration et ne redémarre aucun service NUT.

```sh
wol ups test
```

Ce diagnostic relit uniquement les UPS configurés. Il distingue notamment timeout, port fermé, service non-NUT, accès refusé, UPS inconnu et absence de `ups.status`. Les métriques de batterie restent facultatives.

Les états reconnus sont `OL`, `OB`, `LB`, `FSD` et `unknown`, ainsi que `battery.charge` et `battery.runtime`. Une politique applicable bloque le réveil si l'état est inconnu ou la communication impossible.

Pour un serveur NUT distant, activer `tls: true` : le client négocie `STARTTLS`, vérifie le nom du serveur et accepte une CA privée via `ca_cert`. Une connexion distante en clair est refusée sauf `allow_insecure_remote: true`, réservé à un tunnel ou réseau administratif déjà protégé. Les identifiants sont lus depuis des fichiers privés.

Un retour secteur automatisable est une transition réellement observée vers `OL`, pas l'état `OL` initial au lancement.
