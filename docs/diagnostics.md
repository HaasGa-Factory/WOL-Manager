# Diagnostics réseau

La découverte passive lit interfaces, adresses et, sous Linux, `ip -j route` et `ip -j neigh`. Elle n'émet aucun paquet.

La découverte active demande une activation YAML et `--allow-active-scan`. Elle limite cumulativement sous-réseaux, paquets, débit et durée. `--dry-run` produit le plan sans émission. `Ctrl-C` ou le timeout annule l'opération.

Chaque résultat distingue :

- `connectivity` : route ou socket disponible ;
- `packet` : bloqué, simulé, émis ou en erreur ;
- `reception` : confirmation instrumentée ou inconnue ;
- `wake` : machine observée joignable ou non observée.

Le test actif peut réveiller une machine. Il n'est jamais lancé par une automatisation, une reprise ou un échec de réveil.
