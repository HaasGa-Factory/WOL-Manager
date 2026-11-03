# Diagnostics

## Mise à jour refusée pour configuration incompatible

Si `sudo wol update --channel dev` signale une configuration incompatible, consultez le journal
indiqué par la commande. Si le journal mentionne `unicast` et que la commande suivante annonce
l'installateur `1.2.1`, appliquez d'abord l'amorçage vérifié de `docs/updates.md` :

```sh
sudo /usr/local/lib/wol-manager/install.sh --version
```

Ne copiez pas un `install.sh` non vérifié. Le fichier attendu est `dist/install.sh` du paquet `1.2.3`,
après validation de la signature du manifeste avec la clé publique déjà installée et comparaison de
son SHA-256 signé.

Une fois l'installateur système `1.2.3` en place, relancez la mise à jour dans un terminal, acceptez
l'ouverture de la migration guidée, vérifiez qu'un broadcast ou relais utilisable reste configuré,
puis confirmez le résumé. La configuration originale reçoit une sauvegarde datée avant l'écriture.

Si l'installateur `1.2.2` a déjà été copié et échoue parce que
`packaging/logrotate/wol-manager` est absent, le paquet stable `WOL-Manager-main` ne contient pas ce
fichier. Copiez le fichier depuis le paquet `dev` correspondant, vérifiez que son SHA-256 vaut
`cead19ee7daf701832736c08859abe0199391d57270facc9baf65738ad87a500`, puis installez-le ainsi :

```sh
sudo install -d -o root -g root -m 0755 \
  /usr/local/lib/wol-manager/packaging/logrotate
sudo install -o root -g root -m 0644 \
  ./packaging/logrotate/wol-manager \
  /usr/local/lib/wol-manager/packaging/logrotate/wol-manager
sudo wol update --channel dev
```

Le correctif `1.2.3` rend cette ressource optionnelle pendant la transition depuis l'ancien
agencement, afin qu'une ressource auxiliaire manquante ne bloque plus le remplacement du binaire.

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
