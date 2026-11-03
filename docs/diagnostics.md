# Diagnostics

## Mise à jour refusée pour configuration incompatible

Si `sudo wol update --channel dev` signale une configuration incompatible, consultez le journal
indiqué par la commande. Depuis `1.2.2`, la présence de l'ancien transport `unicast` est reconnue :
dans un terminal interactif, acceptez l'ouverture de la migration guidée, vérifiez qu'un broadcast
ou relais utilisable reste configuré, puis confirmez le résumé. La configuration originale reçoit
une sauvegarde datée avant l'écriture.

Avec `--non-interactive`, cette migration est volontairement refusée sans modifier la configuration
ni remplacer le binaire. Relancez alors la commande sans cette option :

```sh
sudo wol update --channel dev
```

## Diagnostics réseau

La découverte passive lit interfaces, adresses et, sous Linux, `ip -j route` et `ip -j neigh`. Elle n'émet aucun paquet.

La découverte active demande une activation YAML et `--allow-active-scan`. Elle limite cumulativement sous-réseaux, paquets, débit et durée. `--dry-run` produit le plan sans émission. `Ctrl-C` ou le timeout annule l'opération.

Chaque résultat distingue :

- `connectivity` : route ou socket disponible ;
- `packet` : bloqué, simulé, accepté par le socket ou en erreur ;
- `reception` : confirmation instrumentée ou inconnue ;
- `wake` : machine observée joignable ou non observée.

Un succès de `WriteToUDP` ne constitue pas une preuve de présence du paquet sur le câble. Sans
capture ou observation équivalente, l'émission reste non vérifiée. Les résultats de `wol wake`
exposent séparément `socket_accepted`, `packet_emitted` et `wake_verified`.

Le test actif peut réveiller une machine. Il n'est jamais lancé par une automatisation, une reprise ou un échec de réveil.

Pour diagnostiquer la reprise NUT sans modifier l'UPS :

```sh
wol ups test
wol automation status
```

Le premier affiche les états et mesures réellement lisibles. Le second indique le cycle enregistré, les groupes armés/terminés, la stabilité secteur, les tâches et la raison d'une attente. Une batterie inconnue, un état `OB`, `LB`, `FSD` ou `unknown` doit rester visible comme blocage ; ce n'est pas une valeur zéro implicite.
